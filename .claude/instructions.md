# Claude Code Instructions for Sentinel

## Code Style & Patterns

### Go
- Immutable data patterns — never mutate structs in place
- Error handling at every layer — no silent failures
- Small functions (<50 lines) and focused modules (<800 lines)
- Use interfaces for dependency injection (e.g., datasource.Source, llm.Provider)
- Table-driven tests for logic validation

### TypeScript/React
- Server components by default in Next.js
- Use `force-dynamic` pages when fetching real-time data
- Type all API responses (lib/api.ts has the shapes)
- Keep components small and composable
- No prop drilling — use layout tree for shared state

### Database
- PostgreSQL with pgvector for semantic search
- Always write migrations (up/down) for schema changes
- Use parameterized queries (no SQL injection)
- Index foreign keys and frequently filtered columns

## Before Writing Code

1. **Read existing patterns** — Find a similar feature and follow its structure
2. **Write tests first** — Use table-driven tests for logic
3. **Check for circular imports** — Go doesn't allow them (common with notify ↔ investigation)
4. **Validate config defaults** — NewX() functions should set sensible defaults

## Common Pitfalls to Avoid

1. **Circular imports**: `investigation` can't import `notify.Payload` directly → define it in `notify` package
2. **Blocking operations**: Long-running tasks (embedding, HTTP) should be async with goroutines
3. **SQL injection**: Always use parameterized queries
4. **Unhandled errors**: Don't return nil error if something might fail
5. **Hardcoded secrets**: Use env vars or secrets manager

## File Organization

```
internal/alert/
├── model.go          # Alert types (Event, Severity, Source)
├── repository.go     # Database access (CRUD, queries)
├── normalizer.go     # Transform raw payloads → Event
├── dedup.go          # Fingerprint-based deduplication
└── source/
    ├── source.go     # Handler interface
    ├── webhook.go    # Generic webhook parser
    ├── slack.go      # Slack Socket Mode listener
    ├── outlook.go    # IMAP email poller
    ├── prometheus.go # Alertmanager v4 parser
    └── grafana.go    # Grafana unified alerting parser
```

Keep handler logic separate from storage logic. Each source should be independently testable.

## Testing Strategy

- **Unit tests**: Pure functions (parsers, filters, severity mapping)
- **Integration tests**: Database queries, repository methods
- **Mock external services**: Use test doubles for HTTP (httptest), channels, etc.
- **Coverage**: Aim for 80%+ on critical paths

### Test file naming
- `file_test.go` — tests for corresponding `file.go`
- `*_test.go` — tests within the same package

### Test structure
```go
func TestFeatureBehavior(t *testing.T) {
  // Arrange
  input := "test"
  expected := "result"

  // Act
  got := Feature(input)

  // Assert
  if got != expected {
    t.Errorf("expected %q, got %q", expected, got)
  }
}
```

## Debugging Hints

- **Migrations not applying?** Check `migrations/` directory order (timestamp-based)
- **Alert not triggering?** Check `alert.Normalize()` — might be treated as duplicate
- **Investigation stuck?** Check LLM API key config and timeout in investigation/engine.go
- **UI fetch failing?** Check CORS headers and API baseURL in web/.env.local

## Performance Considerations

- **Embedding is async** — non-blocking after alert save
- **pgvector queries** — use `LIMIT 10` for Top-K similarity search
- **Runbook matching** — O(runbooks) per alert, acceptable for <1000 runbooks
- **Redis** — optional, used for caching (not yet integrated)

## Security Notes

- Never log API keys or secrets
- Validate all user input (Runbook YAML, alert webhooks)
- HMAC signing for DingTalk webhooks (done in notify/dingtalk.go)
- Environment variables override config files for sensitive values

## When to Use Agents

| Agent | Situation |
|-------|-----------|
| code-reviewer | After writing non-trivial code changes |
| go-reviewer | After modifying Go code (concurrent, error handling) |
| security-reviewer | Before committing auth, secrets, or user input handling |
| tdd-guide | Starting a new feature or bug fix |
| architect | Planning major refactors or new subsystems |

## Commit Message Style

```
<type>(<scope>): <subject>

<optional body>

<optional footer>
```

Types: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `perf`, `ci`

Example:
```
feat(alert): add Prometheus Alertmanager webhook support

- Parse v4 webhook format
- Extract title from annotations.summary
- Map severity labels to internal levels
- Add comprehensive tests

Closes #123
```
