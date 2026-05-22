# Pricing Data

Beacon estimates token costs only when captured events do not already include
provider-reported cost data. The estimate uses `internal/capture.PricingTable`,
whose values are standard API input and output prices per 1 million tokens.

The table was last verified on May 22, 2026 against these provider sources:

- Anthropic model IDs and versioning:
  <https://platform.claude.com/docs/en/about-claude/models/model-ids-and-versions>
- Anthropic pricing:
  <https://platform.claude.com/docs/en/about-claude/pricing>
- OpenAI pricing:
  <https://developers.openai.com/api/docs/pricing>

Only canonical provider model IDs belong in the table. Compatibility aliases
such as `claude-opus-4`, `claude-sonnet-4`, and `claude-haiku-4` are deliberately
not keyed because aliases can drift or be retired. Unknown model IDs and removed
aliases use the configured fallback prices.

## Fallback Behavior

When a model is not present in `PricingTable`, Beacon calculates cost from:

```toml
[pricing]
default_input_cost = 3.00
default_output_cost = 15.00
```

These defaults are also stored per 1 million tokens. Change them in
`~/.beacon/beacon.toml` or a file passed with `--config` when a local deployment
uses models that Beacon does not know yet.

## Updating Prices

1. Verify current standard API input and output pricing from the official
   provider docs above.
2. Update `PricingTable` with canonical model IDs only. Do not add convenience
   aliases unless the provider documents that the ID itself is a pinned model ID.
3. Update `PricingLastVerified` and this document's verification date.
4. Update tests in `internal/capture/pricing_test.go`.
5. Run `go test ./internal/capture` and `go test ./...`.
