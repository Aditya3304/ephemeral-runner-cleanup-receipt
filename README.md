# Ephemeral Runner Cleanup Receipt

A local, offline-capable cleanup evidence pipeline for disposable Kubernetes CI
runners. Built with Go, PostgreSQL, a TypeScript guard, a trusted Sigstore finalizer,
S3-compatible evidence storage and a React dashboard.

**Current milestone: d — trusted finalizer, protected archive and private signing complete.**
PostgreSQL, cleanup CLI, Node 24 guard, local coordinator, independent observer,
private Sigstore and MinIO archive work locally. Six real lifecycle scenarios now
produce verified signed cleanup receipts. API ingestion, dashboard and watchdog
remain pending. No hosted GitHub workflows or AWS resources are running.

On this prepared laptop, run `bash dev ci-up`, `bash dev finalizer-up`, then
`bash dev finalizer-demo` for passing/failing commands, automatic cleanup,
protected archival, keyless signing and independent verification. See the
[milestone d validation report](docs/finalizer-validation.md),
[finalizer guide](docs/finalizer.md), [archive guide](docs/archive.md), and
[private signing guide](infra/sigstore/README.md). Milestone e awaits confirmation.

For the CLI, run `bash dev cli-demo` after `bash dev cli-prepare` and
`bash dev kind-up`. See the [CLI guide](docs/cli.md) and
[CLI validation record](docs/cli-validation.md).

For automatic job cleanup, see the [local CI guide](docs/local-ci.md) and
[guard contract](docs/guard.md). `bash dev ci-demo` runs a passing and a failing
local job after one-time `bash dev ci-prepare` setup. Both should invoke cleanup;
this guard-only demo produces unsigned preliminary evidence. Use
`bash dev finalizer-demo` for the complete signed-receipt path.
The [milestone c validation record](docs/guard-validation.md) records all six live
scenarios and limitations at that historical milestone.

## Run the foundation

Use Ubuntu in WSL with Docker Desktop running. Open this repository in the WSL
terminal. Required for initial setup: Go 1.27.1, Docker with Compose, and internet
access to download pinned dependencies. No paid account is needed.

```bash
bash dev prepare   # One-time downloads and build
bash dev up        # Generate local secrets, start PostgreSQL, migrate, show status
bash dev check     # Real database integration checks in a disposable test database
bash dev demo      # Synthetic metadata transaction, explicitly rolled back
bash dev status    # Version, verified TLS, schema version and persisted row counts
bash dev stop     # Stop the database; keep all volumes
```

After `prepare`, `up`, `check`, `demo` and `status` use local assets. Compose has
`pull_policy: never`; the database network is internal with no published ports.
`check` builds with the Go proxy and checksum network lookups disabled.

`demo` is a schema demonstration, not cleanup proof: it inserts a synthetic failure
and paired incident in one transaction, checks the constraints, then rolls both
back. No synthetic signed receipts are persisted. `finalizer-demo` produces real
signatures and archived receipts; API/database ingestion is the next milestone.

## What exists

- PostgreSQL **17.11**, pinned to an immutable official image digest.
- Goose **3.28.0**, embedded SQL migrations, pgx/v5 **5.10.0**.
- `evidence.receipts`, `evidence.incidents`, `evidence.finalization_attempts`.
- Run/job uniqueness, verdict/coverage rules, immutable object-reference shape,
  atomic receipt/incident pairing, bounded retries and lease metadata.
- Completion/filter indexes, GIN text search and partial incident/retry indexes.
- Separate administrative and restricted API roles; TCP requires TLS and SCRAM.
- A local CA, passwords and server key stored in Docker volumes, outside Git.
- Go tooling, pinned Node 24 runtime, and explicit Action/dashboard boundaries.
- Go/Cobra/client-go `proofctl`: begin, inventory, cleanup, receipt and verify.
- Dedicated kind cluster with UID-bound cleanup, safe filesystem deletion and
  explicit partial/failed preliminary evidence.
- Pinned Cosign, genuine offline bundle checks and private keyless signing services.
- Node 24 guard main/post, local staging and a durable Go CI coordinator.
- Restricted runners with a verified network startup gate, resource quotas and
  protected staging metadata; cancellation and restart recovery.
- Independent stopped-runner filesystem inspection and complete-log observation,
  with atomic readiness publication and honest missing/failed coverage.
- An isolated finalizer, versioned/encrypted MinIO archive with COMPLIANCE retention,
  durable retry/replay handling, and real private Sigstore receipt verification.

## Trust and limitations

SQL constraints do **not** perform cryptographic verification. The `verified`
signature-state constraint is a structural guard. Only milestone e's ingestion API
will be permitted to set it after genuine bundle, identity and artifact validation.
Administrative dbtool and isolated test fixtures intentionally have privileges
that will never be provided to job runners.

The runtime API will mount only `api-secrets`, never administrative secrets or the
database server private key. It cannot update/delete receipts, delete incidents,
create tables, administer users, or change Goose migration history.

The local-only scope replaces live GitHub execution with real local Kubernetes
jobs, public Sigstore with a private instance, and AWS S3 with local MinIO. GitHub
is a source publication destination. GitHub/AWS live validation is explicitly
deferred; their behavior cannot be claimed from local tests.

See [database design](docs/database.md), [milestone status](docs/milestones.md),
and the [approved plan](docs/approved-build-plan.md).

## Repository layout

```text
action/             TypeScript guard workspace (milestone c)
cmd/dbtool/         Working database migration/status/demo command
dashboard/          React workspace (milestone f)
db/                 Embedded Goose migrations and real database tests
docs/               Approved scope, design and milestone evidence
infra/postgres/     TLS-only database access policy
internal/database/  Credential-file loading and verify-full connections
internal/dbfixture/ Synthetic, isolated schema-test data only
scripts/            Local bootstrap and database initialization
compose.yaml        Private local database and one-shot admin tools
dev                 Repeatable WSL/Linux commands
```

`internal/proof/` contains cleanup, canonical evidence and signature verification;
`cmd/proofctl/` exposes the CLI. `scripts/demo-cli.py` runs the real demonstration.

Never remove the project's Docker volumes to fix a routine startup problem. They
contain persistent data and the local database identity. Startup preserves existing
secrets and refuses to overwrite a partially generated identity. No destructive
reset command is provided.
