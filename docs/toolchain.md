# Toolchain and dependency updates

Beacon keeps CI tool versions explicit so local development and pull-request
checks fail for the same reasons.

## Version sources

- Go: `go.mod` `go` directive. GitHub Actions reads this with
  `actions/setup-go` `go-version-file`.
- templ: `go.mod` `tool github.com/a-h/templ/cmd/templ`. Run with
  `make generate` or `go tool templ generate`; use `make generate-check` to
  verify generated output is current.
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
  Dashboard/search and accessibility suites are portable Chromium checks.
  Visual regression snapshots are Darwin Chromium baselines and remain
  local-only until the project maintains CI-compatible baselines or a stable
  macOS visual runner.

## Update process

1. Update one tool or dependency group at a time.
2. Regenerate the relevant lock or generated files:
   - Go modules: `go get` or `go mod tidy`;
   - templ output: `make generate`;
   - npm packages: `npm install`;
   - vendored browser assets: `npm run vendor`.
3. Run the relevant local checks:
   - `make fmt-check`;
   - `go test ./...`;
   - `govulncheck ./...`;
   - `golangci-lint run ./...`;
   - `npm audit --audit-level=moderate`;
   - Playwright suites when npm or frontend assets changed.
     Use `npm run test:visual -- --update-snapshots` only when intentionally
     accepting visual changes on Darwin.
4. Review generated, vendored, and lockfile diffs before opening a PR.

Do not replace pinned CI tool versions with floating aliases such as `latest`.

## Pull-request hygiene gates

CI runs these hygiene gates on pull requests:

- `format`: `make fmt-check` fails on tracked Go files with gofmt drift.
- `generated`: `make generate-check` fails if generated templ files are stale
  or missing from the commit.
- `test`: `go test -race -coverprofile=coverage.txt -covermode=atomic ./...`
  runs Go tests and `scripts/check-coverage.sh` enforces package coverage
  floors.
- `lint`: `golangci-lint run ./...` fails on configured Go lint issues.
- `build`: `go build ./cmd/beacon` verifies the CLI binary builds after
  template generation.
- `govulncheck`: `govulncheck ./...` fails on reachable Go vulnerabilities.
- `npm-audit`: `npm audit --audit-level=moderate` fails on moderate or higher
  npm advisories.
- `dependency-review`: GitHub dependency review fails pull requests introducing
  moderate or higher dependency vulnerabilities.
- `playwright-dashboard`: `npm run test:e2e` runs dashboard and dashboard
  search browser workflows, uploading Playwright reports and trace/media
  outputs on failure.
- `playwright-accessibility`: `npm run test:a11y` runs axe-backed browser
  checks, uploading the same Playwright artifacts on failure.

Run the matching local commands before opening dependency, generated-code, or
toolchain PRs.

For template changes, commit both the `.templ` source and the matching
`_templ.go` output. Keep component tests focused on rendered HTML behavior,
escaping, and helper-visible output; coverage gates intentionally avoid
package floors for generated templ packages so tests do not chase generated
line coverage.

The visual Playwright suite is excluded from CI by design while only Darwin
baselines are checked in. Keep that exclusion explicit in PRs that touch
`tests/e2e/visual.spec.ts` or its snapshots, and document any future move to
Linux or hosted macOS visual baselines in the same change that enables the CI
job.
