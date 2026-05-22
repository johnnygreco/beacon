# Toolchain and dependency updates

Beacon keeps CI tool versions explicit so local development and pull-request
checks fail for the same reasons.

## Version sources

- Go: `go.mod` `go` directive. GitHub Actions reads this with
  `actions/setup-go` `go-version-file`.
- templ: `go.mod` `tool github.com/a-h/templ/cmd/templ`. Run with
  `go tool templ generate`.
- golangci-lint: `.github/workflows/ci.yml` `GOLANGCI_LINT_VERSION`.
  CI must not use `latest`; update intentionally after a local lint pass.
- govulncheck Go: `.github/workflows/ci.yml` `GOVULNCHECK_GO_VERSION`.
  This should stay on a patched Go release so standard-library vulnerability
  checks start from a fixed toolchain.
- govulncheck: `.github/workflows/ci.yml` `GOVULNCHECK_VERSION`.
  CI must not use `latest`; update intentionally after a local vulnerability
  scan.
- Node.js: `.github/workflows/ci.yml` `NODE_VERSION`. Node is required for npm
  scripts and Playwright tests.
- npm packages: `package-lock.json`. Update with `npm install` and review the
  lockfile diff.
- Playwright: `package-lock.json` via `@playwright/test`. Browser install
  happens in CI with `npx playwright install --with-deps chromium`.

## Update process

1. Update one tool or dependency group at a time.
2. Regenerate the relevant lock or generated files:
   - Go modules: `go get` or `go mod tidy`;
   - templ output: `go tool templ generate`;
   - npm packages: `npm install`;
   - vendored browser assets: `npm run vendor`.
3. Run the relevant local checks:
   - `make fmt-check`;
   - `go test ./...`;
   - `govulncheck ./...`;
   - `golangci-lint run ./...`;
   - `npm audit --audit-level=moderate`;
   - Playwright suites when npm or frontend assets changed.
4. Review generated, vendored, and lockfile diffs before opening a PR.

Do not replace pinned CI tool versions with floating aliases such as `latest`.

## Pull-request hygiene gates

CI runs these hygiene gates on pull requests:

- `format`: `make fmt-check` fails on tracked Go files with gofmt drift.
- `generated`: `go tool templ generate` fails if generated templ files are
  stale or missing from the commit.
- `govulncheck`: `govulncheck ./...` fails on reachable Go vulnerabilities.
- `npm-audit`: `npm audit --audit-level=moderate` fails on moderate or higher
  npm advisories.
- `dependency-review`: GitHub dependency review fails pull requests introducing
  moderate or higher dependency vulnerabilities.

Run the matching local commands before opening dependency, generated-code, or
toolchain PRs.
