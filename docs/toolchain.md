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
- Vendored browser assets: `scripts/vendor-assets.mjs` copies npm package
  assets into `internal/assets/static/` and records package versions, licenses,
  upstreams, and file hashes in `internal/assets/static/vendor-manifest.json`.
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
   - `make test-cover`;
   - `GOTOOLCHAIN=go1.26.4 make vulncheck`;
   - `make lint`;
   - `npm run vendor:check`;
   - `npm audit --audit-level=moderate`;
   - Playwright suites when npm or frontend assets changed.
     Use `npm run test:visual -- --update-snapshots` only when intentionally
     accepting visual changes on Darwin.
4. Review generated, vendored, manifest, notice, and lockfile diffs before
   opening a PR.

Do not replace pinned CI tool versions with floating aliases such as `latest`.

## Pull-request hygiene gates

CI runs these hygiene gates on pull requests:

- `format`: `make fmt-check` fails on tracked Go files with gofmt drift.
- `generated`: `make generate-check` fails if generated templ files are stale
  or missing from the commit.
- `test`: `make test-cover` runs Go tests with the race detector and atomic
  coverage, then `scripts/check-coverage.sh` enforces package coverage floors.
- `lint`: `make lint` fails on configured Go lint issues.
- `build`: `go build .` verifies the root-installable CLI binary builds after
  template generation, and CI also builds `./cmd/beacon` for legacy install
  compatibility.
- `govulncheck`: `make vulncheck` fails on reachable Go vulnerabilities.
  Run it under the patched Go version pinned by `GOVULNCHECK_GO_VERSION` when
  your local default `go` is older, for example
  `GOTOOLCHAIN=go1.26.4 make vulncheck`.
- `npm-audit`: `npm audit --audit-level=moderate` fails on moderate or higher
  npm advisories.
- `frontend`: `npm run vendor:check` fails if vendored browser assets,
  `internal/assets/static/vendor-manifest.json`, or `THIRD_PARTY_NOTICES.md`
  are stale, or if the vendor directories contain unconfigured files. Then
  `npm run test:frontend` runs frontend lint and unit tests.
- `dependency-review`: GitHub dependency review fails pull requests introducing
  moderate or higher dependency vulnerabilities.
- `playwright-dashboard`: `npm run test:e2e` runs dashboard and dashboard
  search browser workflows, uploading Playwright reports and trace/media
  outputs on failure.
- `playwright-accessibility`: `npm run test:a11y` runs axe-backed browser
  checks, uploading the same Playwright artifacts on failure.

Run the matching local commands before opening dependency, generated-code, or
toolchain PRs.

The Makefile package list is based on tracked `.go` files in Git checkouts so
ignored dependency trees such as `node_modules` are not swept into local Go
test, lint, or vulnerability commands. Source archive builds discover Go files
from the extracted tree while pruning ignored dependency and build directories
before validating package candidates with `go list`.

For template changes, commit both the `.templ` source and the matching
`_templ.go` output. Keep component tests focused on rendered HTML behavior,
escaping, and helper-visible output; coverage gates intentionally avoid
package floors for generated templ packages so tests do not chase generated
line coverage.

For vendored browser asset changes, commit the updated files under
`internal/assets/static/js/vendor` or `internal/assets/static/css/vendor`, the
regenerated `internal/assets/static/vendor-manifest.json`, `package-lock.json`
when package versions changed, and `THIRD_PARTY_NOTICES.md` when package
version, license, upstream, or copyright text changed. `npm run vendor:check`
compares each vendored file to the installed npm package source and verifies
the manifest plus notices mention every vendored file, package version,
license, and upstream. It also fails on unconfigured files left in
`internal/assets/static/js/vendor` or `internal/assets/static/css/vendor`.
If a dependency changes license, update `scripts/vendor-assets.mjs` only after
reviewing the new license and notice text.

The visual Playwright suite is excluded from CI by design while only Darwin
baselines are checked in. Keep that exclusion explicit in PRs that touch
`tests/e2e/visual.spec.ts` or its snapshots, and document any future move to
Linux or hosted macOS visual baselines in the same change that enables the CI
job.
