# Contributing

Thanks for helping improve Endurance Rewards! This guide keeps contributions consistent and easy to review.

## Ground rules
- Use Go 1.23+ and keep changes small and focused.
- Add/adjust tests when changing behavior; do not disable existing tests.
- Follow existing patterns for logging (`log/slog`), HTTP handlers (Gin), and config (`internal/config`).
- No secrets in the repo. Keep real values in `.env` or your shell.

## Project setup
1. Fork and clone the repo, then `cd endurance-rewards`.
2. Install tools: Go 1.23+, `make`, `docker` (optional), [`golangci-lint`](https://golangci-lint.run/usage/install/), and [`swag`](https://github.com/swaggo/swag) for Swagger generation.
3. Fetch dependencies:
   ```bash
   make deps
   ```

## Development workflow
- Build and regenerate Swagger docs: `make build`
- Run the service: `make run`
- Run tests: `make test`
- Lint: `make lint`
- Format Go files before committing: `gofmt -w <files>` (or `go fmt ./...`).

## Pull requests
- Describe the problem and solution; include screenshots for UI changes.
- Note how you tested (commands, environments) and any follow-up items.
- Keep diffs minimal; prefer multiple small PRs over a large one.
- Update docs/README when user-visible behavior or configuration changes.

## Commit style
- Use concise messages (e.g., `Add address rewards endpoint`, `Fix backfill retry logic`).
- One logical change per commit; avoid bundling unrelated fixes.
