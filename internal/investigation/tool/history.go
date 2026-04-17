package tool

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sentinelai/sentinel/internal/alert"
	"github.com/sentinelai/sentinel/internal/llm"
)

var SearchHistoryTool = llm.Tool{
	Name:        "search_history",
	Description: "Search historical alerts similar to the current one. Returns past root causes and resolutions that may be relevant to the current investigation. Uses semantic vector search when available, falling back to keyword search.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"query":   {"type": "string", "description": "Keywords or description to search for in historical alert titles and descriptions"},
			"service": {"type": "string", "description": "Filter by service name (optional)"},
			"limit":   {"type": "integer", "description": "Max results to return (default 5)"}
		},
		"required": ["query"]
	}`),
}

type SearchHistoryInput struct {
	Query   string `json:"query"`
	Service string `json:"service"`
	Limit   int    `json:"limit"`
}

// SearchHistory returns a tool handler that queries historical alerts.
// When embedder is non-nil it performs semantic vector search; otherwise
// falls back to ILIKE keyword search.
// alertRepo is used for vector search; db is used for keyword fallback and temporal.
func SearchHistory(db *sql.DB, alertRepo *alert.Repository, embedder llm.Embedder) Func {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var in SearchHistoryInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", fmt.Errorf("invalid input: %w", err)
		}
		if in.Limit == 0 {
			in.Limit = 5
		}

		var parts []string

		// --- 1. Vector semantic search ---
		if embedder != nil {
			vec, err := embedder.Embed(ctx, in.Query)
			if err == nil && len(vec) > 0 {
				similar, err := alertRepo.FindSimilar(ctx, vec, in.Limit)
				if err == nil && len(similar) > 0 {
					var lines []string
					for _, s := range similar {
						if s.Similarity < 0.75 {
							continue // skip low-relevance results
						}
						line := fmt.Sprintf("- [%.0f%% match] [%s] %s (service: %s)",
							s.Similarity*100,
							s.ReceivedAt.Format("2006-01-02 15:04"),
							s.Title, s.Service,
						)
						if s.RootCause != "" {
							line += fmt.Sprintf("\n  Root cause: %s", s.RootCause)
						}
						if s.Resolution != "" {
							line += fmt.Sprintf("\n  Resolution: %s", s.Resolution)
						}
						lines = append(lines, line)
					}
					if len(lines) > 0 {
						parts = append(parts, "## Semantically Similar Historical Alerts\n"+strings.Join(lines, "\n\n"))
					}
				}
			}
		}

		// --- 2. Keyword fallback (always runs as supplement) ---
		keywordResults := searchByKeyword(ctx, db, in.Query, in.Service, in.Limit)
		if keywordResults != "" {
			parts = append(parts, "## Keyword-Matched Historical Alerts\n"+keywordResults)
		}

		// --- 3. Temporal correlation (same service ±15min) ---
		if in.Service != "" && alertRepo != nil {
			temporal := temporalCorrelation(ctx, alertRepo, in.Service)
			if temporal != "" {
				parts = append(parts, "## Correlated Alerts (same service, last 15 min)\n"+temporal)
			}
		}

		if len(parts) == 0 {
			return "No similar historical alerts found.", nil
		}
		return strings.Join(parts, "\n\n"), nil
	}
}

func searchByKeyword(ctx context.Context, db *sql.DB, query, service string, limit int) string {
	const q = `
		SELECT a.title, a.service, a.received_at,
		       COALESCE(i.root_cause,''), COALESCE(i.resolution,''), COALESCE(i.summary,'')
		FROM alerts a
		LEFT JOIN investigations i ON i.alert_id = a.id AND i.status = 'completed'
		WHERE (a.title ILIKE $1 OR a.description ILIKE $1)
		  AND ($2 = '' OR a.service = $2)
		ORDER BY a.received_at DESC
		LIMIT $3`

	like := "%" + escapeILIKE(query) + "%"
	rows, err := db.QueryContext(ctx, q, like, service, limit)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var results []string
	for rows.Next() {
		var title, svc, rootCause, resolution, summary string
		var receivedAt time.Time
		if err := rows.Scan(&title, &svc, &receivedAt, &rootCause, &resolution, &summary); err != nil {
			continue
		}
		line := fmt.Sprintf("- [%s] %s (service: %s)",
			receivedAt.Format("2006-01-02 15:04"), title, svc)
		if rootCause != "" {
			line += fmt.Sprintf("\n  Root cause: %s", rootCause)
		}
		if resolution != "" {
			line += fmt.Sprintf("\n  Resolution: %s", resolution)
		}
		results = append(results, line)
	}
	if len(results) == 0 {
		return ""
	}
	return strings.Join(results, "\n\n")
}

// escapeILIKE escapes special ILIKE/LIKE characters so they match literally.
func escapeILIKE(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func temporalCorrelation(ctx context.Context, repo *alert.Repository, service string) string {
	events, err := repo.FindByServiceInWindow(ctx, service, time.Now(), 15*time.Minute)
	if err != nil || len(events) == 0 {
		return ""
	}

	var lines []string
	for _, e := range events {
		lines = append(lines, fmt.Sprintf("- [%s] [%s] %s",
			e.ReceivedAt.Format("15:04:05"),
			string(e.Severity),
			e.Title,
		))
	}
	return strings.Join(lines, "\n")
}
