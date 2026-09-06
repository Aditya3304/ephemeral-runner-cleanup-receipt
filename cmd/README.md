# Command boundaries

- `dbtool`: implemented foundation tooling; embeds Goose migrations.
- `proofctl`: implemented milestone b, Go/Cobra/client-go cleanup and offline Cosign verification.
- `coordinator`: milestone c, local Kubernetes job scheduling and durable run ledger.
- `finalizer`: milestone d, trusted archival and real private Sigstore signing.
- `api`: milestone e, Chi/pgx API and signature verification before ingestion.
- `watchdog`: milestone g, retry and unresolved-finalization detection.

dbtool and proofctl are executable at milestone b. dbtool's administrative and test privileges
are not a template for the runtime API's credentials.
