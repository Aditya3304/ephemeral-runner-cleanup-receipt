# Milestone status

The user approved the solo, offline, zero-spend build plan on September 7, 2026.
Each completed milestone is demonstrated and requires confirmation before the next.

| Gate | Status | Scope |
|---|---|---|
| Plan approval | Approved | Local Kubernetes CI, private Sigstore, local MinIO/PostgreSQL; no paid cloud resources |
| a: foundation | Complete; milestone b approved | Repository, schema/migrations, TLS, restricted database roles and repeatable local commands |
| b: cleanup CLI | Complete; milestone c approved | Cobra/client-go commands, real kind cleanup, canonical preliminary evidence and offline Sigstore verification |
| c: guard/coordinator | Complete; milestone d approved | Local job lifecycle, durable coordinator, isolated runners and disabled GitHub adapter |
| d: signing/finalizer | Complete; awaiting approval for e | Independent observations, private keyless signing, protected archive and verified canonical receipts |
| e: API | Not started | Chi/pgx verification, sanitization, transactional ingestion and queries |
| f: dashboard | Not started | React/Vite/Tailwind/TanStack search/detail/incident UI |
| g: watchdog | Not started | Missing-finalization recovery and incidents |
| h: hardening | Not started | End-to-end fault and offline-execution matrix |
| i: local deployment | Not started | Packaging, backup and rehearsals; AWS remains excluded |

## Milestone d validation and remaining work

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
