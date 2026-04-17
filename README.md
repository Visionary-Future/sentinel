# Sentinel

**AI-driven alert investigation platform for Chinese cloud environments.**

Sentinel automatically executes runbooks when alerts fire, investigates root causes using AI agents connected to your observability data, and delivers rich investigation reports to your team via WeCom, DingTalk, or Slack — before your engineers even open their laptops.

## Why Sentinel

| Pain Point | How Sentinel Helps |
|---|---|
| Alert fatigue | AI resolves 70%+ of alerts automatically, humans only handle escalations |
| Slow MTTR | Investigation starts in seconds, not minutes |
| Knowledge silos | Runbooks encode senior engineer expertise; AI follows them 24×7 |
| Missing context | Historical alert correlation surfaces past root causes instantly |
| China cloud gap | Native support for Aliyun SLS/CMS, Huawei LTS/CES, WeCom, DingTalk |

## Architecture

```
Alert Sources          Core Engine                 Output
─────────────          ───────────                 ──────
Slack Channel  ──┐     ┌─────────────┐             WeCom
Outlook Email  ──┼────►│ Alert       │◄── Runbook  DingTalk
Aliyun Alert   ──┤     │ Normalizer  │    Registry Slack
Huawei Alert   ──┤     └──────┬──────┘             Outlook Reply
Webhook        ──┘            │
                       ┌──────▼──────┐
                       │ Correlation │ ← pgvector similarity search
                       │ Engine      │   (historical alert context)
                       └──────┬──────┘
                       ┌──────▼──────┐
                       │ AI Agent    │ ── query_logs    ──► Aliyun SLS
                       │ (ReAct loop)│ ── query_metrics ──► Aliyun CMS
                       │             │ ── search_history──► PostgreSQL
                       └──────┬──────┘ ── search_code  ──► GitHub
                       ┌──────▼──────┐
                       │ Investigation│
                       │ Notebook    │ ← Web UI (coming soon)
                       └─────────────┘
```

## Features

- **Multi-source alert ingestion** — Slack, Outlook, Aliyun EventBridge, Huawei SMN, generic Webhook
- **Markdown Runbooks** — write investigation playbooks in plain Markdown; AI follows them step-by-step
- **LLM-agnostic** — Claude (Anthropic), Tongyi Qwen (Aliyun), DeepSeek, or any OpenAI-compatible model
- **Aliyun SLS integration** — query logs directly from Log Service during investigations
- **Historical alert correlation** — surfaces past root causes before the agent starts digging
- **WeCom & DingTalk notifications** — rich investigation reports delivered to your team chat
- **Human-in-the-loop** — dangerous remediation actions require approval before execution
- **Enterprise ready** — self-hosted, bring-your-own LLM, SOC2-compatible deployment

## Quick Start

### Prerequisites

- Docker + Docker Compose
- Go 1.23+ (for local development)

### 1. Clone and configure

```bash
git clone https://github.com/sentinelai/sentinel.git
cd sentinel
cp configs/sentinel.yaml configs/sentinel.local.yaml
```

Edit `configs/sentinel.local.yaml` — at minimum set your LLM API key:

```yaml
llm:
  default_provider: claude
  providers:
    claude:
      api_key: sk-ant-...
      model: claude-sonnet-4-6
```

### 2. Start infrastructure

```bash
make docker-up   # starts PostgreSQL (with pgvector) + Redis
```

### 3. Run Sentinel

```bash
make run
# or: go run ./cmd/sentinel -config configs/sentinel.local.yaml
```

```
{"level":"INFO","msg":"database migrations applied"}
{"level":"INFO","msg":"starting HTTP server","addr":":8080"}
```

### 4. Create your first Runbook

```bash
curl -X POST http://localhost:8080/api/v1/runbooks \
  -H "Content-Type: text/plain" \
  --data-binary @- <<'EOF'
# API Latency Runbook

## Trigger
- alert.title contains "latency"
- alert.severity in [critical, warning]
- alert.service matches "order-*"

## Steps
- Query logs for $service in the last 30 minutes, look for errors and exceptions
- Check P99 latency metrics and compare to 7-day baseline
- Search for similar historical latency alerts and their resolutions
- Check recent deployments for $service in the last 24 hours

## Escalation
- team: platform-team
- channel: wecom://your-webhook-url
- timeout: 30m
EOF
```

### 5. Send a test alert

```bash
curl -X POST http://localhost:8080/api/v1/alerts/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "title": "[FIRING] order-service P99 latency > 2s",
    "severity": "critical",
    "service": "order-service",
    "description": "P99 latency exceeded threshold for 5 minutes",
    "labels": {"env": "prod", "region": "cn-hangzhou"}
  }'
```

Sentinel will:
1. Match the alert to your runbook
2. Start an AI investigation automatically
3. Send the report to WeCom/DingTalk when done

---

## Configuration

Full reference for `configs/sentinel.yaml`:

```yaml
server:
  port: 8080
  mode: debug  # or release

database:
  host: localhost
  port: 5432
  name: sentinel
  user: sentinel
  password: sentinel
  sslmode: disable

# ── Alert Sources ──────────────────────────────────────────
alert_sources:
  slack:
    enabled: false
    bot_token: "xoxb-..."        # Bot User OAuth Token
    app_token: "xapp-..."        # App-Level Token (Socket Mode)
    signing_secret: "..."
    channels:
      - id: C01ABCDEF01
        name: "#ops-alerts"
        filters:
          keywords: ["FIRING", "alert", "告警"]

  webhook:
    enabled: true
    path: /api/v1/alerts/webhook
    secret: ""                   # optional HMAC signing secret

# ── Data Sources ───────────────────────────────────────────
data_sources:
  aliyun_sls:
    enabled: false
    endpoint: "cn-hangzhou.log.aliyuncs.com"
    access_key_id: ""
    access_key_secret: ""
    project: "my-project"
    default_logstore: "app-logs"

  aliyun_cms:
    enabled: false
    access_key_id: ""
    access_key_secret: ""
    region: "cn-hangzhou"

# ── LLM ───────────────────────────────────────────────────
llm:
  default_provider: claude       # claude | tongyi | deepseek | openai
  providers:
    claude:
      api_key: ""
      model: claude-sonnet-4-6
    tongyi:
      api_key: ""
      model: qwen-max
    deepseek:
      api_key: ""
      model: deepseek-chat

# ── Notifications ──────────────────────────────────────────
notification:
  wecom:
    enabled: false
    webhook_url: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=..."

  dingtalk:
    enabled: false
    webhook_url: "https://oapi.dingtalk.com/robot/send?access_token=..."
    secret: ""                   # optional signing secret

  slack:
    enabled: false
    bot_token: "xoxb-..."
    default_channel: "C01ABCDEF01"
    reply_in_thread: true        # reply in the original alert thread
```

All config values can be overridden via environment variables:

```bash
SENTINEL_LLM_PROVIDERS_CLAUDE_API_KEY=sk-ant-...
SENTINEL_DATABASE_PASSWORD=mysecretpassword
SENTINEL_NOTIFICATION_WECOM_WEBHOOK_URL=https://...
```

---

## Writing Runbooks

Runbooks are plain Markdown files with three sections:

### Trigger

Defines which alerts activate this runbook. Supports four operators:

| Operator | Example | Description |
|----------|---------|-------------|
| `contains` | `alert.title contains "latency"` | Case-insensitive substring match |
| `equals` | `alert.severity equals "critical"` | Exact match |
| `matches` | `alert.service matches "order-*"` | Glob pattern |
| `in` | `alert.severity in [critical, warning]` | Value in list |

Available fields: `alert.title`, `alert.description`, `alert.severity`, `alert.service`, `alert.source`, `alert.labels.<key>`

### Steps

Free-form investigation instructions. The AI agent reads these and decides which tools to call. Use `$service` and `$alert.title` as placeholders — the agent fills them in from the alert context.

```markdown
## Steps
- Query error logs for $service in the last 30 minutes
- If error rate > 5%, check which endpoints are affected
- Compare current P99 latency to 7-day baseline
- Search for similar historical alerts and past root causes
- Check recent deployments — look for regressions in error-prone paths
```

### Escalation

```markdown
## Escalation
- team: platform-team
- channel: wecom://your-group
- timeout: 30m
```

### Full Example

```markdown
# Database Connection Pool Exhaustion

## Trigger
- alert.title contains "connection pool"
- alert.service matches "*.service"

## Steps
- Query database connection logs for errors in the last 15 minutes
- Check current connection count vs pool maximum
- Identify which services are consuming the most connections
- Check if a recent deployment changed connection pool settings
- Search history for similar connection pool exhaustion incidents

## Escalation
- team: database-team
- channel: wecom://db-oncall
- timeout: 15m
```

---

## AI Tools

The AI agent has access to these built-in tools during investigation:

| Tool | Description |
|------|-------------|
| `query_logs` | Search and analyse logs from configured data sources (SLS, ES) |
| `query_metrics` | Fetch time-series metrics (CMS, CES) |
| `search_history` | Find similar historical alerts with their root causes |
| `analyze_service_deps` | Traverse service dependency graph for cascade failures *(coming soon)* |
| `check_deployments` | Look up recent code deployments *(coming soon)* |

---

## API Reference

### Alerts

```
POST /api/v1/alerts/webhook        Ingest a generic JSON alert
POST /api/v1/alerts/alertmanager   Ingest Prometheus Alertmanager webhook (v4)
POST /api/v1/alerts/grafana        Ingest Grafana unified alerting webhook
GET  /api/v1/alerts                List alerts (paginated)
GET  /api/v1/alerts/:id            Get alert details
```

**Generic webhook payload:**

```json
{
  "title":       "string (required)",
  "severity":    "critical | warning | info",
  "service":     "string",
  "description": "string",
  "labels":      { "key": "value" }
}
```

**Alertmanager** sends its native v4 format directly — no transformation needed.

**Grafana** sends its unified alerting format directly — no transformation needed.

### Runbooks

```
POST   /api/v1/runbooks            Create runbook (body = Markdown text)
GET    /api/v1/runbooks            List enabled runbooks
GET    /api/v1/runbooks/:id        Get runbook details
DELETE /api/v1/runbooks/:id        Disable runbook
```

### Investigations

```
GET  /api/v1/investigations/:id    Get investigation with steps and report
```

**Investigation response:**

```json
{
  "ID":          "uuid",
  "AlertID":     "uuid",
  "RunbookID":   "uuid",
  "Status":      "pending | running | completed | failed",
  "RootCause":   "string",
  "Resolution":  "string",
  "Summary":     "string",
  "Steps":       [ { "index": 1, "description": "...", "tool_calls": [...] } ],
  "LLMProvider": "claude",
  "LLMModel":    "claude-sonnet-4-6",
  "TokenUsage":  1234
}
```

---

## Development

```bash
# Install tools
go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest
brew install golangci-lint

# Run tests
make test

# Lint
make lint

# Create a new migration
make migrate-create   # prompts for migration name

# Apply migrations manually
make migrate-up

# Roll back one migration
make migrate-down
```

### Project Structure

```
sentinel/
├── cmd/sentinel/          Entry point
├── internal/
│   ├── alert/             Alert model, normalizer, dedup, repository
│   │   └── source/        Slack, Webhook, Outlook, Aliyun, Huawei adapters
│   ├── runbook/           Markdown parser, matcher, repository
│   ├── investigation/     Agent loop, engine, repository
│   │   └── tool/          query_logs, query_metrics, search_history
│   ├── datasource/        Unified data source interface
│   │   └── aliyun/        Aliyun SLS, CloudMonitor
│   ├── llm/               Claude, Tongyi, OpenAI-compat providers
│   ├── notify/            WeCom, DingTalk, Slack, Outlook notifiers
│   ├── catalog/           Service catalog and dependency graph
│   ├── config/            Config loading (Viper)
│   ├── store/             PostgreSQL connection and migrations
│   └── api/               Gin HTTP server, handlers, middleware
├── migrations/            SQL migration files
├── configs/               Configuration templates
├── deploy/                Docker Compose, Helm charts
└── docs/                  Architecture and design documents
```

---

## Roadmap

### MVP (current)
- [x] Webhook + Slack alert ingestion
- [x] Email alert ingestion via IMAP (163企业邮箱, QQ企业邮箱, Outlook, Gmail, etc.)
- [x] Markdown Runbook engine with trigger matching
- [x] AI Agent with ReAct loop (Claude, Tongyi, DeepSeek)
- [x] Historical alert correlation (pgvector semantic search)
- [x] Aliyun SLS log integration
- [x] Aliyun CloudMonitor (CMS) metrics integration
- [x] WeCom + DingTalk + Slack notifications
- [x] Investigation Notebook web UI

### Next
- [ ] Huawei Cloud LTS + CES integration
- [ ] Human-in-the-loop approval for remediation actions
- [ ] Service catalog and dependency traversal
- [ ] Aliyun ARMS distributed trace analysis
- [ ] Real-time investigation streaming (SSE)

---

## License

MIT
