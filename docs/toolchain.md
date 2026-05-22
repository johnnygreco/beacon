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
   - `golangci-lint run ./...`;
   - `npm audit --audit-level=moderate`;
   - Playwright suites when npm or frontend assets changed.
4. Review generated, vendored, and lockfile diffs before opening a PR.

Do not replace pinned CI tool versions with floating aliases such as `latest`.
