# Milestone status

The user approved the solo, offline, zero-spend build plan on September 7, 2026.
Each completed milestone is demonstrated and requires confirmation before the next.

| Gate | Status | Scope |
|---|---|---|
| Plan approval | Approved | Local Kubernetes CI, private Sigstore, local MinIO/PostgreSQL; no paid cloud resources |
| a: foundation | Complete; milestone b approved | Repository, schema/migrations, TLS, restricted database roles and repeatable local commands |
| b: cleanup CLI | Complete; milestone c approved | Cobra/client-go commands, real kind cleanup, canonical preliminary evidence and offline Sigstore verification |
| c: guard/coordinator | Complete; milestone d approved | Local job lifecycle, durable coordinator, isolated runners and disabled GitHub adapter |
| d: signing/finalizer | Complete; milestone e approved | Independent observations, private keyless signing, protected archive and verified canonical receipts |
| e: API | Complete; milestone f approved | Chi/pgx verification, sanitization, atomic receipts/incidents, queries and durable delivery |
| f: dashboard | Complete; milestone g approved | AFTER: React/Vite/Tailwind/TanStack, interactive coverage, search/filters/incidents and fresh verification |
| g: watchdog | Complete; milestone h approved | Persistent WSL recovery, bounded retries, crash/outage reconciliation and recovery visibility |
| h: hardening | Complete; milestone i approved | End-to-end fault, trust, service-outage, offline-execution and browser matrix |
| i: local deployment | Complete | Offline start/stop/status package, verified backup/restore, two rehearsals, sample evidence and recording |

## Milestone i validation and completed release

Release `v0.1.0` starts and stops the whole prepared local system without deleting
state or downloading runtime assets. A 41,994-byte PostgreSQL custom backup was
restored and identity-checked three times in disposable databases. Two independent
stopped-to-ready rehearsals each produced one passing receipt and one interrupted
failure receipt with exactly one incident; all signatures, archived artifacts and
resource absence checks passed. A real 1.7 MiB Chromium recording and two screenshots
show the second rehearsal in AFTER with external requests blocked.

The retained release dossier contains tagged source, pinned identities, contract
hashes, backup proof, both rehearsal reports and passing/failing signed samples.
See [release-validation.md](release-validation.md) and
[local-release.md](local-release.md). All approved local acceptance requirements
are complete. Hosted GitHub Actions, public Sigstore and AWS remain excluded under
the user's fully local, zero-spend scope.

## Historical milestone h validation

All sixteen local hardening groups passed. Six fresh lifecycle cases produced
signed, archived, independently verified receipts; the real MinIO, signing issuer,
PostgreSQL, archive-read, and Kubernetes control-plane outages recovered without
duplicate receipts or job command replay. Tampering, identity mismatch, unsafe
ownership, residue, missing evidence, log gaps, restricted credentials and forced
database rollback all stayed fail-closed. All 17 dashboard browser checks passed.

The matrix found and fixed one real defect: loss of the node collector's private
result directory was endlessly retryable. The repaired coordinator now distinguishes
that irreversible gap from a temporary outage, disposes only UID-bound resources,
and issues a signed partial receipt with one incident. See
[hardening-validation.md](hardening-validation.md).

At the milestone h gate, the local ledger contained 33 verified receipts: 26 pass,
three fail and four partial, with seven paired incidents. Milestone i has since
completed the release, backup, rehearsal, sample-evidence and recording work.

## Historical milestone g validation

The installed WSL user service recovered missing finalizations, a real API outage
and a killed coordinator without rerunning its job. The full local ledger sweep
accounts for 37 runs: 21 independently verified receipts (18 pass, two fail, one
partial), and 16 incompatible historical input records retained for review.
The three non-passing receipts have three paired cleanup incidents. Operational
recovery failures are shown separately from signed cleanup verdicts.

See [watchdog-validation.md](watchdog-validation.md) and [watchdog.md](watchdog.md).
The dashboard includes recovery states, retained failure reasons and navigation
to verified receipts. The service is installed by content digest and preserves
retry budgets across restarts. All runtime components remain on this laptop.

Milestone h subsequently completed the full system fault/offline acceptance matrix,
and milestone i completed local packaging, backup and rehearsal. Hosted GitHub,
public Sigstore and AWS remain excluded.

## Historical milestone f validation (at its completion)

AFTER is running at http://localhost:8080/ with seven genuine receipts and one
open incident. A fresh Kubernetes run was cleaned, independently observed,
archived, signed, ingested and freshly verified from the browser. All 14 Chromium
checks passed, including real verification, error states, filters, keyboard access,
light/dark accessibility scans and narrow-screen reflow.

See [dashboard-validation.md](dashboard-validation.md) and
[dashboard.md](dashboard.md). The UI includes an interactive five-check map,
exact artifact references and recorded timeline; summary counts describe only the
loaded view. All UI assets are local. Analytics charts, live polling and incident
editing remain outside the approved v1 design.

At that milestone, automatic recovery, full-system fault/offline acceptance,
backup and rehearsal remained pending. Milestone g has since been approved and
completed as recorded above.

## Historical milestone e validation and deferrals (at its completion)

Six genuine signed local runs have independently verified API records: five
cleanup passes and one failure with one linked incident. Concurrent duplicate
insertion, conflicting versions, tampered signatures, sanitized search/filter
queries and forced transaction rollback were tested. A real database outage
preserved the signed output and recovered through durable delivery after an API
restart. The API's own network namespace permits PostgreSQL and MinIO while
blocking signing-service and external connectivity.

See [api-validation.md](api-validation.md) and [api.md](api.md). The Windows
loopback gateway exposes JSON at `http://localhost:8080/v1/receipts`. React UI,
scheduled watchdog/leases, full-system fault coverage, backup and rehearsal remain
pending. The user must approve f before dashboard work begins. Hosted GitHub,
public Sigstore and AWS remain outside the approved runtime scope.

## Historical milestone d validation and deferrals (at its completion)

Six real jobs completed cleanup, trusted observation, encrypted/versioned archive,
private keyless signing, network-disabled CLI verification and independent object
readback. Five have signed passing cleanup verdicts; abrupt termination has a
signed failure recording residual files. Replayed finalization retained the same
receipt/bundle versions. See [finalizer-validation.md](finalizer-validation.md)
for exact identities, hashes, test results and limits.

The private signing and archive services run on this laptop with no public runtime
dependencies or paid resources. API ingestion and incident transactions, React
dashboard, watchdog, full system fault matrix, and local backup/rehearsal remain
pending. No application receipt has yet been written to PostgreSQL. Hosted GitHub
and AWS remain outside the approved scope. Confirmation is required before e.

## Historical milestone c validation and deferrals (at its completion)

All six live lifecycle scenarios passed: success, command failure, cancellation,
missing post after abrupt runner termination, coordinator restart and isolation.
The guard's 21 tests, Go checks, Go vet, and all four original live CLI cleanup
groups passed. See [guard-validation.md](guard-validation.md) for exact run IDs,
image/source identity, the retained cancellation retry diagnostic and test limits.

The local runner and its test resources were independently confirmed absent after
each case. Evidence is still unsigned and partial; collected logs are an unattested
snapshot. No verified receipt is yet stored or visible in a dashboard. Private
Sigstore, MinIO and the trusted finalizer are next, followed by the API, dashboard,
watchdog, full fault matrix and local packaging. Hosted GitHub and AWS remain
outside the approved offline runtime scope.

## Historical milestone b validation and deferrals (at its completion)

The real CLI demo, filesystem/identity safety tests, four live Kubernetes scenarios,
offline Sigstore positive/negative checks and Go vet passed. See
[cli-validation.md](cli-validation.md) and [cli.md](cli.md) for evidence and commands.

- Cleanup evidence is unsigned and partial. Logs and runner disposal remain unobservable.
- Cosign 3.1.3 is now pinned locally for verification. Private signing is milestone d.
- The developer kind cluster is real; restricted CI jobs and the guard/coordinator are c.
- MinIO archival, trusted finalizer, API, dashboard, watchdog and system fault tests remain pending.
- The full network-blocked application demonstration remains pending; no live GitHub or AWS runtime is used.

## Historical milestone a deferrals (at its completion)

- No Kubernetes cluster or runner created yet.
- No signing, object archive, API or dashboard is claimed to work.
- Node 24 workspace metadata is present, but Node installation/builds belong to
  the guard/dashboard milestones. The host currently has Node 22.
- The installed development Cosign binary still needs provenance/version pinning
  at the signing milestone.
- Terraform/AWS provisioning and real hosted GitHub workflows remain excluded by
  the approved offline scope.
- SQL receipts used in schema checks are synthetic. Integration checks isolate
  them in a temporary database; the local demo rolls them back.

Actual validation results are recorded in [foundation-validation.md](foundation-validation.md).
