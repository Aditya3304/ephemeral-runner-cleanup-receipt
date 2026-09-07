# Milestone e — verified API and delivery

Validated on the user's Windows/WSL laptop on September 7, 2026. Milestone e is
complete; milestone f requires confirmation before implementation.

The Go API independently re-verifies genuine private Sigstore receipts and every
archived artifact, derives coverage from archived observations, sanitizes metadata,
and commits receipts/incidents through the restricted PostgreSQL role. The local
gateway is reachable from Windows at
[receipt metadata](http://localhost:8080/v1/receipts).

## Real stored results

These are genuine milestone d receipts, not synthetic signatures. All six were
independently verified and ingested; their exact archive versions were preserved.

| Local job | Cleanup verdict | PostgreSQL receipt UUID |
|---|---|---|
| Passing command | pass | `e2d5e27a-cd7d-43bd-b4c2-c3ed59afba89` |
| Failing command | pass | `d96e0252-a7be-4f40-92b3-7734b01354fe` |
| Graceful cancellation | pass | `82272386-f677-43fc-974a-9aacdac20d4c` |
| Abrupt termination / missing post | fail | `f3aefeb6-81cf-4cb2-8d67-7836394d6a18` |
| Coordinator restart | pass | `ab23cbe3-06c6-49d0-b767-1eed0f7b071e` |
| Runner isolation | pass | `9e74651b-0074-474a-b0a5-e422cff23b26` |

The failed cleanup has exactly one incident:
`35114dbc-dc9c-4265-b2e5-bb587d0524d0`. A failed application command is distinct
from failed cleanup, which explains the passing cleanup verdict for that command.
The main database contains six receipts, one incident and one operational retry
report after the demo. Synthetic concurrent/rollback fixtures stayed in a separate
randomly named `proofcheck_api_*` database, removed by the test cleanup.

## Validation performed

| Check | Observed result |
|---|---|
| Genuine signature and exact artifact verification | Passed for all six real receipts, both admission and fresh API verification |
| Modified canonical receipt and modified bundle | Rejected by genuine Cosign verification before insertion |
| Wrong issuer, identity, revision and trust-root policy | Rejected |
| Twelve concurrent database submissions of one verified failure | One receipt, one incident, one created response, eleven duplicates |
| Different immutable bundle version at the same identity | Conflict; existing record preserved |
| Deliberately failing incident trigger | Whole transaction rolled back; no orphan receipt |
| Sanitization | Credential-like text and HTML in free-form evidence reasons absent from database metadata |
| Repository, verdict, incident, date and text filters | Passed |
| Bounded keyset pagination | Two pages returned different records without repetition |
| API database permissions and TLS | Forbidden updates/deletes/schema creation rejected; TLS connection confirmed |
| Delivery restart, lost commit response and retry cap | Unit tests passed; exact references reconciled without an extra insertion |
| Real PostgreSQL outage and API restart | Persisted retry recovered on delivery attempt two; same signed output retained |
| Real MinIO outage | Fresh verification returned 503; metadata unchanged; identical archive references verified after restart |
| HTTP authorization, body limits, noncanonical input, identity mismatch | Live API rejected invalid submissions |
| API network namespace | Database/archive positive controls passed; issuer/Fulcio/Rekor and external IP connectivity blocked |
| Go regression tests and static checks | Passed for existing internal/command packages and new API/delivery code |
| Windows loopback access | `http://localhost:8080/readyz` returned ready |

The database outage used run `e80b6858da20377c88d4c0c0a9ae4ba3`. Delivery attempt one
persisted a retry due at `2026-09-07T00:50:14.774993645Z`. The API was restarted
while PostgreSQL was down, PostgreSQL was restored, and attempt two committed
receipt `ab23cbe3-06c6-49d0-b767-1eed0f7b071e`. Replays returned that same UUID.
The failed attempt was then recorded through the authenticated API.

The archive-outage check used receipt
`9e74651b-0074-474a-b0a5-e422cff23b26`. Verification returned
`dependency_unavailable` while MinIO was stopped. After restart it verified the
same receipt SHA-256
`e5f67125cdd0cf80d7f95cd1e032c86d366e80cecd2bced4a384e3f320427c26`.

## Reproduction and saved evidence

```sh
bash dev api-build
bash dev api-up
bash dev api-check
bash dev api-demo
bash scripts/api-live-test.sh
python3 scripts/api-archive-fault.py
bash dev status
```

The demo performs its database outage only when a genuine fixture is still
awaiting ingestion. Subsequent runs replay existing deliveries and preserve the
original outage result. The archive-fault command briefly stops only this
project's MinIO and restores it in a `finally` block. No test deletes application
objects, signing keys, archive volumes or the main database.

Private local validation records (excluded from Git):

- `.build/api-validation.log`: real archive/Cosign checks and isolated PostgreSQL
  concurrency, conflict, rollback, sanitization, query and privilege checks.
- `.build/api-live-validation.log`: actual service network namespace and HTTP checks.
- `.build/api-regression.log`: existing Go regression and static-check results.
- `.build/api-demo-results.json`: six real receipt UUIDs, fresh verification results
  and original durable database-outage recovery details.
- `.build/api-archive-fault.json`: archive interruption and restored verification.

Final installed API/gateway image:
`sha256:c0cad39d05a3c154c71285512fcac02a8648d31015bbd337c8824e068900cddb`.
The finalizer's approved signing revision remains
`b42fd6d4496336233d9d490531ca2a6265932505`; API work does not relabel historical
receipts. The trust-root digest remains
`fbbd3e48e79bc80e3dad572fb9865289a0654a7de4a67edca09fdd79369afb70`.

The Windows output directory includes `Run-verified-receipt-API.cmd` and a public
copy of the milestone result JSON. The launcher starts the prepared database,
archive and API, then opens the local metadata endpoint. It requires no signing
service for historical verification and makes no dependency downloads.

## Scope and practical limits

The runtime API has no database administrator credential, archive write identity,
signing private identity, Kubernetes config or Docker socket. A separate gateway
without secret mounts provides the Windows loopback port. Active signing services
are unnecessary for API verification because Cosign verifies the detached bundle
offline. See [API guide](api.md) for contracts, permissions and bounded retries.

The UI is currently JSON. React/Vite/Tailwind/TanStack dashboard work has not begun.
Delivery replay is explicit; scheduled watchdog discovery and leases are milestone
g. Full fault-matrix completion, fresh signed partial scenarios, backup, packaging
and rehearsal are still pending. This validates local PostgreSQL/MinIO/private
Sigstore, not hosted GitHub, public Sigstore or AWS. No paid resources were used.
