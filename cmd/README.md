# Command boundaries

- `dbtool`: implemented foundation tooling; embeds Goose migrations.
- `proofctl`: implemented milestone b, Go/Cobra/client-go cleanup and offline Cosign verification.
- `localci`: milestone c, local Kubernetes job scheduling and durable run ledger.
- `netgate`: trusted local kind-node helper; pins a runner network namespace and installs verified filtering before user code starts.
- `finalizer`: milestone d, trusted archival and real private Sigstore signing.
- `api`: milestone e, Chi/pgx API and signature verification before ingestion.
- `watchdog`: milestone g, installed WSL service, bounded retries and unresolved-finalization detection.
- `recoverybridge`: isolated authenticated recovery reporting and exact published-reference reconciliation.

dbtool, proofctl and localci are executable. netgate is installed only on the dedicated
kind node. dbtool's administrative and test privileges
are not a template for the runtime API's credentials.
