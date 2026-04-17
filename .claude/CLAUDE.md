# Sentinel Project Guide

**AI-driven alert investigation platform for Chinese cloud environments.**

## Project Overview

Sentinel automatically executes runbooks when alerts fire, investigates root causes using AI agents, and delivers rich investigation reports via WeCom, DingTalk, or Slack.

- **Backend**: Go 1.25+, Gin, PostgreSQL (with pgvector), Redis
- **Frontend**: Next.js 15, TypeScript, Tailwind CSS
- **LLM**: Claude (Anthropic), Tongyi Qwen (Aliyun), DeepSeek
- **Alert Sources**: Slack, Email (IMAP), Webhook, Prometheus Alertmanager, Grafana, Aliyun EventBridge
- **Data Sources**: Aliyun SLS (logs), Aliyun CloudMonitor (metrics)
- **Notifications**: WeCom, DingTalk, Slack

## Key Directories

```
sentinel/
├── cmd/sentinel/              # Entry point
├── internal/
│   ├── alert/                 # Alert ingestion & models
│   │   └── source/            # Slack, Email, Webhook, Prometheus, Grafana, etc.
│   ├── investigation/         # AI agent & investigation engine
│   │   └── tool/              # Agent tools (query_logs, query_metrics, search_history)
│   ├── datasource/            # Data source adapters (SLS, CMS)
│   ├── notify/                # Notification channels (WeCom, DingTalk, Slack, Email)
│   ├── runbook/               # Markdown runbook parser & matcher
│   ├── llm/                   # LLM providers (Claude, Tongyi, DeepSeek)
│   ├── api/                   # HTTP API handlers
│   └── store/                 # Database & migrations
├── web/                       # Next.js frontend
├── migrations/                # SQL schema migrations
└── configs/                   # Configuration templates
```

## Important Files

### Config
- `configs/sentinel.yaml` — Main config template
- Environment variables: `SENTINEL_*` prefix (e.g., `SENTINEL_LLM_PROVIDERS_CLAUDE_API_KEY`)

### Database
- PostgreSQL schema migrations in `migrations/`
- pgvector extension for semantic alert correlation
- Run migrations automatically on startup

### Alert Sources
- **Webhook**: `POST /api/v1/alerts/webhook`
- **Alertmanager**: `POST /api/v1/alerts/alertmanager`
- **Grafana**: `POST /api/v1/alerts/grafana`
- **Slack**: Socket Mode (incoming alerts) + Web API (outgoing notifications, separated)
- **Email**: IMAP poller (163, QQ, Outlook, Gmail, etc.)

### Web UI
- Pages: Dashboard, Alerts, Investigations, Runbooks
- Server components with `force-dynamic`
- API client in `web/lib/api.ts`

## Development Workflow

### Setup
```bash
make docker-up          # Start PostgreSQL + Redis
make run               # Run backend
cd web && npm run dev  # Run frontend (separate terminal)
```

### Testing
```bash
make test              # Run all Go tests
make lint              # Lint Go code
cd web && npm run test # Jest tests (if configured)
```

### Adding a Feature

1. **Plan first**: Read architecture.md and existing patterns
2. **TDD approach**: Write test → implement → verify
3. **Code review**: Use code-reviewer agent before pushing
4. **Database changes**: Create migration with `make migrate-create`
5. **Commit**: Follow conventional commits (feat:, fix:, docs:, etc.)

## Code Quality Guidelines

- **Immutability**: Never mutate existing objects, return new copies
- **Error handling**: Handle all errors explicitly, never silently swallow
- **Input validation**: Validate at system boundaries (user input, API responses)
- **File size**: Keep files <800 lines, prefer small focused modules
- **Functions**: Keep functions <50 lines
- **No hardcoded values**: Use constants or config

## Alert Flow

```
Alert Sources (Slack, Email, Webhook, Prometheus, Grafana)
    ↓
Alert Normalizer (extract title, severity, service)
    ↓
Alert Repository (save to PostgreSQL)
    ↓
Async Embedding (pgvector for semantic search)
    ↓
Runbook Matcher (find matching investigation playbooks)
    ↓
Investigation Engine (AI agent with tool execution)
    ↓
Notification Channels (WeCom, DingTalk, Slack, Email)
    ↓
Web UI (Investigation Notebook)
```

## Investigation Agent Tools

- `query_logs` — Search logs from Aliyun SLS
- `query_metrics` — Fetch metrics from Aliyun CloudMonitor
- `search_history` — Find similar past alerts with their root causes
- `search_code` — Search GitHub for recent changes (future)
- `check_deployments` — Look up recent deployments (future)

## LLM Integration

The agent uses Claude by default, but Tongyi and DeepSeek are also supported.

**Switching LLMs**: Set `llm.default_provider` in config to `claude`, `tongyi`, or `deepseek`.

**Multi-model setup**:
```yaml
llm:
  default_provider: claude
  providers:
    claude:
      api_key: sk-ant-...
      model: claude-sonnet-4-6
    tongyi:
      api_key: sk-...
      model: qwen-max
    deepseek:
      api_key: sk-...
      model: deepseek-chat
```

## Common Tasks

### Add a new notification channel
1. Implement `notify.Channel` interface in `internal/notify/`
2. Add config struct to `internal/config/config.go`
3. Add to `configs/sentinel.yaml` template
4. Wire up in `buildNotifier()` in `cmd/sentinel/main.go`

### Add a new data source
1. Implement `datasource.Source` interface in `internal/datasource/`
2. Add config struct to `internal/config/config.go`
3. Wire up in `buildDataSourceRegistry()` in `cmd/sentinel/main.go`

### Modify alert ingestion
1. Add parser in `internal/alert/source/`
2. Add HTTP endpoint in `internal/api/handler/alert.go`
3. Register route in `internal/api/server.go`

### Update database schema
```bash
make migrate-create   # Name the migration descriptively
# Edit migrations/VERSION_description.up.sql
# Edit migrations/VERSION_description.down.sql
make migrate-up       # Apply migrations
```

## Known Limitations

- **No multi-tenancy**: Single tenant per deployment
- **No RBAC**: All users have full access
- **No auto-remediation**: AI can only investigate, not execute actions (future phase)
- **Huawei Cloud**: Not yet supported (roadmap)
- **ARMS traces**: Not yet integrated (roadmap)

## Useful Commands

```bash
# Development
make run              # Run backend with config at configs/sentinel.yaml
make test             # Run all tests with race detector
make lint             # Run golangci-lint
make docker-up        # Start Docker Compose (PostgreSQL, Redis)
make docker-down      # Stop Docker Compose

# UI
cd web && npm run dev     # Start Next.js dev server
cd web && npm run build   # Build production bundle

# Database
make migrate-up           # Apply pending migrations
make migrate-down         # Rollback one migration
make migrate-create       # Create new migration (prompts for name)

# Deployment
docker build -t sentinel:latest .
docker-compose -f deploy/docker-compose.yml up -d
```

## Links

- GitHub: https://github.com/sentinelai/sentinel
- Architecture doc: `docs/architecture.md`
- API Reference: `docs/api.md` (coming soon)

## Getting Help

- Check `docs/` for detailed documentation
- Review existing tests for usage patterns
- Ask Claude Code questions about architecture, patterns, or specific files
