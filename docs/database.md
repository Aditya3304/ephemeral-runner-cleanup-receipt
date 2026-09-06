# Database foundation

## Ownership and transport

The application schema is `evidence`. Only the future API is a runtime database
writer. PostgreSQL's administrative user is confined to one-shot migration/test
tools and container initialization. `proof_api` is a separate restricted role.

No TCP port is published. The internal Compose network has no external gateway.
Network connections require TLS; clients use `sslmode=verify-full` with the local
CA and `db` hostname. Plaintext is explicitly rejected in `pg_hba.conf`. Local
Unix-socket administration uses peer authentication inside the database container.

The official PostgreSQL entrypoint handles initial storage permissions and data
checksums. Credentials are generated with OpenSSL and stored only in named Docker
volumes. The CA private key belongs to the administrative volume; clients receive
the public CA certificate. Windows bind-mount file permissions are not used for
secret protection. Docker administrators remain trusted.

## Tables

| Table | Purpose | Main constraints |
|---|---|---|
| receipts | Sanitized metadata, coverage, signature result, immutable object references | Unique provider/repository/run/attempt/job; valid chronology, verdict, digests, reference shape and coverage |
| incidents | One tracked incident for each failed or partial receipt | Unique receipt and issue key; FK; state/timestamp consistency |
| finalization_attempts | Retry history, current stage/result and lease | Unique job identity plus retry number; max six attempts; valid retry times/leases; linked receipt must belong to the same job |

An external keyless certificate/signature bundle is stored in object storage. It
is not duplicated in PostgreSQL. JSONB fields contain bounded summaries/reference
objects, never raw logs or the full untrusted receipt.

The API must later sanitize text, allowlist stores, validate references against
real objects, re-verify signatures and derive the verdict. Database validation of
metadata shape does not establish those external facts.

## Receipt semantics

Each of workspace, credentials, logs, resources and runner disposal contains:

- `status`: verified, failed, unsupported or unobservable.
- `observer`: trusted, job or none.
- `reason`: bounded explanatory text.

A pass requires all five components to be verified by a trusted observer and at
least one log reference. A fail requires an observed failure. Partial means no
known failed component but incomplete or insufficiently trusted verification.
Missing evidence cannot become a passing receipt.

Object references contain exactly `store`, `bucket`, `key`, `version_id`, `sha256`
and `size_bytes`. Store is a configured alias, not a caller-supplied URL. Version
IDs bind the archived object version; SHA-256 binds its bytes. The API will map
these references to controlled S3-compatible operations and display URIs.

## Transactions and deduplication

Deferred constraint triggers require zero incidents for a passing receipt and
exactly one for a failure/partial receipt at commit. A receipt and incident can
therefore be inserted together; either both commit or neither does. A uniqueness
constraint arbitrates concurrent deliveries. The future API will return the
existing receipt for an exact duplicate and a conflict for different content at
the same identity.

Terminal receipts are immutable to the API role. Transient finalization failures
are retried before a terminal incomplete receipt is committed. A later new CI
attempt gets a new identity; automatic replacement of terminal receipts is not
part of v1.

Incident state is read by joining the incidents table rather than duplicating it
on receipts. This preserves the PDF's incident-state filter without two mutable
copies of the same fact. Only state, resolution time and optional GitHub issue URL
are updatable on an incident; no incident-editing UI is in scope.

## Migrations and recovery

`dbtool migrate` runs embedded Goose SQL in one-shot administrative execution.
The initial migration is reversible for isolated test environments; runtime
startup applies only upward migrations. The test suite uses a uniquely generated
`proofcheck_...` database and drops only that database after its checks.

Goose's tracking table is infrastructure metadata, in addition to the three
application tables. No resource-summary GIN index is added until JSON filtering
is implemented; full-text search has a GIN index now.

Data and secrets survive `bash dev stop` and subsequent `bash dev up`. If secrets
are partially generated, bootstrap stops rather than rotating them silently.
The server certificate is valid for 825 days and the local CA for ten years;
certificate rotation is a later operational task, not an automatic reset.

## Milestone a verification boundaries

The database tests cover migration up/down/up, duplicate identity including
concurrency, atomic failure/incident behavior, rollback, role isolation, verified
TLS and rejected plaintext/wrong CA/wrong hostname/wrong password, coverage and
reference constraints, text search/indexes, and retry/lease/receipt-link rules.

Signature tampering, real evidence digests, MinIO retention, cleanup safety,
offline signing and the dashboard are later milestones. This foundation must not
be represented as passing those acceptance scenarios.
