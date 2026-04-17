package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sentinelai/sentinel/internal/llm"
)

var CheckDeploymentsTool = llm.Tool{
	Name:        "check_deployments",
	Description: "Check recent deployments for a service. Returns deployment history including version, deployer, timestamp, and status. Use this to correlate issues with recent changes.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"service":    {"type": "string", "description": "The service name to check deployments for"},
			"time_range": {"type": "string", "description": "How far back to look, e.g. '2h', '1d'"}
		},
		"required": ["service"]
	}`),
}

type CheckDeploymentsInput struct {
	Service   string `json:"service"`
	TimeRange string `json:"time_range"`
}

// DeploymentSource abstracts deployment history retrieval (GitHub, ArgoCD, EDAS, etc.).
type DeploymentSource interface {
	ListDeployments(ctx context.Context, service string, from, to time.Time) ([]Deployment, error)
}

type Deployment struct {
	Version    string    `json:"version"`
	Deployer   string    `json:"deployer"`
	DeployedAt time.Time `json:"deployed_at"`
	Status     string    `json:"status"` // "success", "failed", "rolling_back"
	CommitSHA  string    `json:"commit_sha"`
	Message    string    `json:"message"`
}

// CheckDeployments returns a handler. If source is nil, returns a stub response.
func CheckDeployments(source DeploymentSource) Func {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var in CheckDeploymentsInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", fmt.Errorf("invalid input: %w", err)
		}
		if in.TimeRange == "" {
			in.TimeRange = "2h"
		}

		if source == nil {
			return fmt.Sprintf(
				"[check_deployments] service=%s time_range=%s\n"+
					"No deployment source configured. Connect GitHub Deployments or ArgoCD in config.",
				in.Service, in.TimeRange,
			), nil
		}

		from, to := parseTimeRange(in.TimeRange)
		deploys, err := source.ListDeployments(ctx, in.Service, from, to)
		if err != nil {
			return "", fmt.Errorf("check_deployments: %w", err)
		}

		if len(deploys) == 0 {
			return fmt.Sprintf("No deployments found for %s in the last %s.", in.Service, in.TimeRange), nil
		}

		var result string
		result = fmt.Sprintf("Found %d deployments for %s in the last %s:\n\n", len(deploys), in.Service, in.TimeRange)
		for _, d := range deploys {
			result += fmt.Sprintf("- [%s] %s by %s (commit: %s) — %s\n",
				d.DeployedAt.Format("2006-01-02 15:04"),
				d.Version, d.Deployer, shortSHA(d.CommitSHA), d.Status,
			)
			if d.Message != "" {
				result += fmt.Sprintf("  Message: %s\n", d.Message)
			}
		}
		return result, nil
	}
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
