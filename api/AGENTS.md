# api/ — CRD Types and Schemas

## What Lives Here
- CRD Go types (spec/status structs)
- Kubebuilder markers (`+kubebuilder:validation`, `+kubebuilder:default`, etc.)
- JSON tags and OpenAPI schema generation

## Invariants
- JSON tags must match field names and `omitempty` rules.
- Optional fields should be pointers or `omitempty` where appropriate.
- Status fields must be write-only from controllers.

## Common Pitfalls
- Forgetting to run generation after type changes.
- Mismatched JSON tag or missing `omitempty`.
- Breaking backward compatibility by removing/renaming fields.

## Commands
- Regenerate CRDs: `./scripts/verify_crd.sh`
- Full repo verify: `make verify-repo`

## Notes
If you update types here, you likely need changes in:
- `internal/controller/` for reconciliation
- `docs/` for user-facing updates
