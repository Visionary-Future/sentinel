package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sentinelai/sentinel/internal/llm"
)

var SearchCodeTool = llm.Tool{
	Name:        "search_code",
	Description: "Search recent code changes (commits, pull requests) for a service or repository. Use this to find what changed recently that might have caused the issue.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"service":    {"type": "string", "description": "Service name or repository to search"},
			"query":      {"type": "string", "description": "Keywords to search in commit messages and PR titles"},
			"time_range": {"type": "string", "description": "How far back to look, e.g. '1d', '7d'"}
		},
		"required": ["service"]
	}`),
}

type SearchCodeInput struct {
	Service   string `json:"service"`
	Query     string `json:"query"`
	TimeRange string `json:"time_range"`
}

// CodeSearchSource abstracts code change retrieval (GitHub, GitLab, etc.).
type CodeSearchSource interface {
	SearchChanges(ctx context.Context, repo string, query string, from, to time.Time) ([]CodeChange, error)
}

type CodeChange struct {
	Type      string    `json:"type"` // "commit" or "pull_request"
	SHA       string    `json:"sha"`
	Title     string    `json:"title"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
	URL       string    `json:"url"`
	FilesChanged int    `json:"files_changed"`
}

// SearchCode returns a handler. If source is nil, returns a stub response.
func SearchCode(source CodeSearchSource) Func {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var in SearchCodeInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", fmt.Errorf("invalid input: %w", err)
		}
		if in.TimeRange == "" {
			in.TimeRange = "1d"
		}

		if source == nil {
			return fmt.Sprintf(
				"[search_code] service=%s query=%s time_range=%s\n"+
					"No code search source configured. Connect GitHub in config (code_sources.github).",
				in.Service, in.Query, in.TimeRange,
			), nil
		}

		from, to := parseTimeRange(in.TimeRange)
		changes, err := source.SearchChanges(ctx, in.Service, in.Query, from, to)
		if err != nil {
			return "", fmt.Errorf("search_code: %w", err)
		}

		if len(changes) == 0 {
			return fmt.Sprintf("No code changes found for %s in the last %s.", in.Service, in.TimeRange), nil
		}

		var result string
		result = fmt.Sprintf("Found %d recent changes for %s:\n\n", len(changes), in.Service)
		for _, c := range changes {
			result += fmt.Sprintf("- [%s] [%s] %s by %s (%d files changed)\n",
				c.CreatedAt.Format("2006-01-02 15:04"),
				c.Type, c.Title, c.Author, c.FilesChanged,
			)
		}
		return result, nil
	}
}
