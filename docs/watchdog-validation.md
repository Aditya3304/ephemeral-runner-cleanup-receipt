# Milestone g validation — local watchdog

Validated on September 7, 2026, on the prepared Ubuntu/WSL laptop. This milestone
implements the approved local replacement for a hosted missing-finalization
scheduler. The source repository is public; application execution uses only the
local Kubernetes, MinIO, private Sigstore, PostgreSQL and API services.

## Observed result

The installed user service is active and enabled, with user lingering enabled.
It reconciled all **37** coordinator ledgers. There are **21 verified receipts**:
18 cleanup passes, two failures and one partial result. The three non-passing
receipts have three paired cleanup incidents.

Of those receipts, seven already existed before milestone g; the watchdog
reconciled them without spending a recovery attempt. Fourteen additional receipts
were recovered during this milestone. The API recovery view therefore records
30 runs: 14 recovered and 16 stopped for incompatible historical evidence.

The 16 older development ledgers omit fields required by the pinned finalizer's
exact canonical contract: `binding_absent`, `role_absent` and `container_id`.
An isolated, network-disabled diagnostic confirmed the
finalizer rejects their trusted ledger format before archival or signing. The
watchdog now detects that condition before dispatching the finalizer, retains all
bytes and prior attempts, and reports `invalid_evidence` for operator review.
Those records are not rewritten or promoted to verified receipts. They are
operational failures, distinct from the three signed cleanup incidents.

## Real recovery demonstrations

| Scenario | Local run | Verified receipt | Observed behavior |
|---|---|---|---|
| Missing finalization | `80302b9aa97c67cf5113af918bbfb151` | `764b8596-b871-4211-93d5-53be57b2aab6` | Worker finalized and delivered on attempt 1; replay retained that attempt and receipt |
| API outage | `fb98a137c72d2f900a09bde429e0f556` | `0c4a4997-baf9-4286-b447-69dc75f7fb01` | Real API stopped; signed output retained; early restart spent no extra attempt; recovery succeeded on attempt 2 after the real deadline |
| Coordinator killed | `d8624fa3a82659f9f9441213f8f6048f` | `c2fb54d8-840b-4885-a491-fcd5ba3c6050` | Owned coordinator process killed after the command; worker resumed collection, confirmed assigned resources absent, then finalized; command ran exactly once |
| Retained retry after installation repair | `34ae46a3ed8372fd77c42392954a46a0` | `35d1e32f-3356-4be5-9940-6fe8868263d8` | Original two failed attempts retained; enabled background service recovered on attempt 3 |

The outage receipt was freshly verified at
`2026-09-07T06:49:55.747141964Z`, with receipt SHA-256
`8096972f9194077e4631a8b0eb9569749af87e5dfac41d6c07b9550f8fe8cfc5`.
The killed-coordinator receipt was freshly verified at
`2026-09-07T07:07:19.697442534Z`, with SHA-256
`2ec46124fc37307649248514c86ce03943aa3b25a3749a5bc8076945a3579df0`.
Both signature and referenced artifacts passed independent verification.

The first outage exercise exposed a Docker image-retention defect: rebuilding a
development tag could make an older installed manifest unavailable. Installation
now retains dedicated tags while executing exact digests. The previously installed
bridge was successfully invoked after a subsequent rebuild, and the affected run
recovered without resetting its retry budget.

The user service initially lacked the Docker supplementary group already present
in terminal sessions. Refreshing the WSL user manager fixed that stale membership;
the installer now checks service-level Docker access before installing. Docker
socket permissions were not weakened.

## Persistence and restart

A deliberate service restart changed the process ID and completed another full
sweep. At `2026-09-07T07:34:04.491851+00:00`, validation confirmed:

- All 37 durable state documents were unchanged.
- All 21 receipt IDs were unchanged.
- No labelled watchdog child containers remained.
- The API still showed 14 recovered and 16 stopped recovery records.
- The service remained active and enabled, with lingering enabled.

The same service process completed its next scheduled sweep at 07:40:05 UTC,
after the five-minute idle interval. It retained the same receipt identities and
attempt numbers, including the stopped historical records.

A process-owned lock is authoritative on this single laptop. The recorded
10-minute lease describes work ownership; it is not a distributed database lock.
The worker checks at startup and five minutes after each completed cycle.
Retry deadlines can therefore wait until the next cycle.

## Automated checks

- Four worker test groups passed: six-attempt budget/backoff/restart/exhaustion,
  interrupted-worker accounting and report replay, permanent-failure reconciliation,
  and lock/corrupt-state/symlink safety.
- The historical-input regression test passed: incomplete canonical fields,
  unexpected fields and changed identities are rejected without rewriting evidence.
- API unit/auth checks and Go vet passed for the worker, API and bridge.
- Six real API integration groups passed against a disposable PostgreSQL database,
  including genuine Cosign verification, concurrent deduplication, transaction
  rollback, query sanitization, restricted privileges, and recovery reporting.
- Recovery integration checks verified that delivery attempt 1 and watchdog
  attempt 1 coexist; unverified success is rejected; old progress cannot downgrade
  success; and a rollback cannot discard watchdog history.
- Real schema integration checks passed through migration 00002, including fresh
  database reset/up, constraints, role permissions and receipt/incident pairing.
- **17 Chromium checks passed, with zero failures or skipped cases**, against the
  populated system. These cover genuine verification, filtering, incidents,
  pagination, local assets, error states, keyboard navigation, light/dark
  accessibility, 320px reflow and the new recovery view.
- Browser accessibility scans reported no WCAG A/AA violations in the tested
  desktop, dark, mobile and expanded recovery states. This is automated evidence,
  not a claim of exhaustive accessibility certification.
- Dashboard formatting, Python compilation and shell syntax checks passed.

Browser mocks are limited to controlled UI failure/empty/pagination states. The
running database contains genuine verified application receipts. The tests use
actual API counts so additional historical recoveries do not invalidate
assumptions inherited from the seven-receipt dashboard milestone.

## Trust and scope retained

The trusted finalizer remains the installed approved revision
`b42fd6d4496336233d9d490531ca2a6265932505`. Its trust policy was not changed.
Public trust root SHA-256:
`fbbd3e48e79bc80e3dad572fb9865289a0654a7de4a67edca09fdd79369afb70`.
Pinned Cosign SHA-256:
`4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71`.

The worker runs compiled installed code, invokes the digest-pinned finalizer,
and uses an isolated bridge for authenticated API updates. Job commands are
never replayed by recovery. A linked receipt must already have passed independent
API verification. The UI does not interpret an operational success as clean
coverage and does not invent receipts for exhausted work.

Detailed selected run identities, installed artifact hashes, service evidence
and browser statistics are exported in the laptop's `milestone-g-results.json`.
The local guide is [watchdog.md](watchdog.md).

## Remaining gate

At the milestone g gate, milestone **h** still required the full end-to-end
cancellation, deletion, signature-tampering, duplicate-delivery, DB/S3-failure
and externally blocked execution matrix. That matrix has since passed; see
[hardening-validation.md](hardening-validation.md).

Milestone **i** covers local packaging, backup and rehearsal. AWS/Terraform cloud
deployment, hosted GitHub Actions and public signing services remain excluded by
the approved zero-spend, offline runtime scope.
