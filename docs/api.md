# Local receipt API

The API independently verifies finalizer submissions, fetches every exact archived
version, and inserts sanitized metadata through the restricted `proof_api` role.
It uses Chi v5.3.2 and pgx/v5. PostgreSQL and the archive remain the durable stores;
no hosted service or paid resource is used at runtime.

## Run on the prepared laptop

Run from the repository in WSL:

```sh
bash dev up
bash dev api-build
bash dev api-up
bash dev api-check
python3 scripts/demo-api.py
```

Open `http://localhost:8080/v1/receipts` in Windows. This milestone exposes JSON;
the React dashboard is milestone f. `bash dev deliver` retries all published
finalizer results, respecting durable retry times. A specific result can be
submitted with `bash dev deliver /input/IDENTITY_HASH/result.json`.

The demo uses genuine archived milestone d runs. On first use, while one run is
still awaiting ingestion, it stops this project's PostgreSQL, attempts delivery,
restarts the API, restores PostgreSQL in a `finally` block, waits for the retry,
and verifies recovery. Existing database contents and archive volumes survive.
Later runs replay delivery without repeating the outage against already ingested
receipts. `.build/api-demo-results.json` records the real outcome.

## Admission boundary

`POST /v1/receipts` accepts the exact canonical `result.json` emitted by the
finalizer (16 KiB maximum, JSON, no compression). The `signature_state` assertion
is only a required transport field. It never authorizes ingestion.

Admission performs these checks before opening the metadata transaction:

1. Exact canonical shape, identity bounds, configured repository, and bounded
   receipt/bundle references. No caller-controlled fetch URL is accepted.
2. Fetch receipt and signature bundle by exact version from the configured archive;
   verify byte lengths, hashes, TLS, encryption and initial retention metadata.
3. Strict receipt parsing, source/job identity matching, and equality to the
   operator's pinned finalizer revision, issuer, signer, trust-root digest,
   signing-config digest and Cosign binary digest.
4. Genuine `cosign verify-blob --offline`, including certificate and bundled
   transparency/timestamp verification. Public trust is mounted read-only;
   the API has no signing identity and cannot reach the signing network.
5. Fetch every referenced ledger, observation, preliminary evidence and log
   version. Re-derive bindings and coverage from those archived bytes and compare
   the resulting canonical receipt. Job assertions retain their untrusted origin.

Archive errors return a retryable failure rather than authorizing a receipt.
Historical reads validate the archive's initial retention guarantees; they do not
renew retention. Only the finalizer's separate write/protection identity can do
that. Admission proves consistency of the signed artifacts with the trusted
finalizer's claims; it does not recreate physical observations after a runner has
been removed.

The API stores constrained identity fields, fixed workflow text, timestamps,
verdict, signature metadata, references, coverage statuses and generated summaries.
It does not store raw commands, arbitrary evidence reasons, credential paths, raw
JSON receipts, logs, or job-provided error text. Raw evidence remains in the
protected archive. JSON responses escape HTML characters; the dashboard must
render metadata as text.

## Transactions and duplicates

One transaction inserts a receipt and, for `partial` or `fail`, exactly one local
incident. Deferred database constraints enforce the pair at commit. A `pass`
cannot have an incident. Any incident failure rolls the receipt back.

The identity is `(provider, repository_id, run_id, run_attempt, job_id)`. For this
single local repository, `repository_id` is the configured repository name. An
exact repeated receipt/bundle reference and source revision returns the existing
UUID. A conflicting reference at that identity returns 409 and never overwrites
the terminal record. A concurrent loser re-reads after the winning transaction
commits. Database failure returns 503; a client must assume a commit response
could have been lost and reconcile or replay safely.

## Endpoints

| Method/path | Result |
|---|---|
| `GET /healthz` | Process liveness |
| `GET /readyz` | Database connectivity; 503 during an outage |
| `POST /v1/receipts` | Authenticated canonical finalization result; 201 created, 200 duplicate |
| `GET /v1/receipts` | Paginated sanitized receipts with nested incident or `null` |
| `GET /v1/receipts/{uuid}` | Receipt metadata and linked incident |
| `GET /v1/receipts/{uuid}/verification` | Fresh signature, artifact and coverage verification |
| `GET /v1/incidents` | Same receipt shape, restricted to receipts with incidents |
| `POST /v1/attempts` | Authenticated immutable completed operational-failure report |
| `GET /v1/attempts?run_id=...` | Authenticated newest 100 attempt reports |

Receipt/incident filters: exact `repository`, `run_id`, `verdict` (`pass`, `fail`,
`partial`), `incident_state` (`open`, `resolved`, `none`), full-text `q`, RFC3339
`from` (inclusive) and `to` (exclusive), `limit` (1–100, default 25), and opaque
`cursor`. Pagination orders by completion time then UUID, both descending. Use
`next_cursor` with the same filters. Unknown/repeated filters are rejected.
No raw SQL, arbitrary sort expression, or artifact URL is accepted.

All mutation and operational-attempt endpoints require `Authorization: Bearer`
with the locally generated 256-bit token in the private Docker auth volume.
Read-only receipt queries are intended for this single-user laptop. Host and
origin checks protect against browser cross-origin access and DNS rebinding;
no permissive CORS is enabled. Responses use `no-store` and `nosniff`.

Verification has a single admission slot with 429 backpressure, a 45-second
request deadline, bounded inputs (1 MiB receipt/JSON artifacts, 2 MiB bundle,
100 MiB logs) and generic error codes. Backend diagnostics and credentials are
never returned to callers. PostgreSQL connections use `verify-full` TLS and a
four-connection pool. The API can start during a database outage and recover
without a restart; readiness remains separate from liveness.

## Durable delivery and attempt reports

The separate `deliver` command reads the finalizer output volume read-only and
persists its canonical queue state in `cleanup-receipt-api_delivery`. It has the
ingestion token but no database or archive credentials. It writes and synchronizes
state before sending requests, locks against concurrent delivery, and rejects
changed input for the same job identity.

Retries use delays of 1, 5, 15, 60 and 60 minutes, at most six delivery attempts.
Both failed and interrupted requests consume attempts. Terminal invalid input
does not retry automatically. Ambiguous successful commits can be reconciled by
exact receipt/bundle references even after the attempt cap. Archived bytes are
never deleted or re-signed because of an ingestion failure. Stored failure reports
are flushed after database recovery, including reports whose retry time is past.

The attempt endpoint accepts exactly `identity`, `attempt_number`, `stage`,
`result`, `error_code`, `next_retry_at`. Stages are `collect`, `archive`, `sign`,
`ingest`; results here are `retry` or `exhausted`; error codes are
`dependency_unavailable`, `invalid_evidence`, `identity_conflict`. Retry requires
a timestamp and attempt 1–5; exhaustion has a null timestamp. The API owns report
timestamps, shifts overdue retry timestamps just beyond report time, and never
replaces an existing attempt's outcome. Success is represented by the verified
receipt. Active leases and automatic scheduled retry selection remain milestone g;
this milestone provides explicit durable replay commands, not a running watchdog.

## Container boundaries

The API is non-root, has a read-only root and mounts, drops all capabilities,
uses a bounded private temporary filesystem, and joins only the internal metadata,
archive and delivery networks. It mounts the restricted database credential copy,
read-only archive verifier credentials, public trust and ingestion auth token.
It has no Docker socket, Kubernetes configuration, administrator password,
finalizer private identity, or archive write identity.

Docker Desktop does not expose published ports on this deployment's all-internal
network configuration. A separate non-root gateway therefore binds Windows
`127.0.0.1:8080` and forwards only to `api:8080`. It has no secret mounts and cannot
select an arbitrary upstream. Its edge network supports the loopback publication;
the private API retains blocked external egress. Neither component makes external
runtime requests. The gateway adds no identity or verification claims.

The integration test container alone mounts admin credentials and has DAC override
to read the different service users' read-only fixture credentials. It creates a
randomly named temporary database and drops only that database during cleanup.
Those elevated test privileges are never granted to the runtime API.

## Deferred scope

Milestone f implements the React UI; g supplies watchdog discovery, scheduling and
leases; h completes the broader fault matrix; i supplies backup and rehearsal
packaging. Hosted GitHub orchestration, public Sigstore, AWS deployment and GitHub
issue publication remain excluded by the approved local/offline scope.

Chi release reference: [upstream v5.3.2](https://github.com/go-chi/chi/releases/tag/v5.3.2).
