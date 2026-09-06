# Command boundaries

- `dbtool`: implemented foundation tooling; embeds Goose migrations.
- `proofctl`: milestone b, Go/Cobra/client-go cleanup and verification.
- `coordinator`: milestone c, local Kubernetes job scheduling and durable run ledger.
- `finalizer`: milestone d, trusted archival and real private Sigstore signing.
- `api`: milestone e, Chi/pgx API and signature verification before ingestion.
- `watchdog`: milestone g, retry and unresolved-finalization detection.

Only dbtool is executable at milestone a. Its administrative and test privileges
are not a template for the runtime API's credentials.
