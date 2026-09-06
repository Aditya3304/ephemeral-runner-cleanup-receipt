# Milestone status

The user approved the solo, offline, zero-spend build plan on September 7, 2026.
Each completed milestone is demonstrated and requires confirmation before the next.

| Gate | Status | Scope |
|---|---|---|
| Plan approval | Approved | Local Kubernetes CI, private Sigstore, local MinIO/PostgreSQL; no paid cloud resources |
| a: foundation | Complete; milestone b approved | Repository, schema/migrations, TLS, restricted database roles and repeatable local commands |
| b: cleanup CLI | Complete; awaiting confirmation for c | Cobra/client-go commands, real kind cleanup, canonical preliminary evidence and offline Sigstore verification |
| c: guard/coordinator | Not started | Local job lifecycle and disabled GitHub adapter |
| d: signing/finalizer | Not started | Local identity/Sigstore, trusted archival, canonical receipt |
| e: API | Not started | Chi/pgx verification, sanitization, transactional ingestion and queries |
| f: dashboard | Not started | React/Vite/Tailwind/TanStack search/detail/incident UI |
| g: watchdog | Not started | Missing-finalization recovery and incidents |
| h: hardening | Not started | End-to-end fault and offline-execution matrix |
| i: local deployment | Not started | Packaging, backup and rehearsals; AWS remains excluded |

## Milestone b validation and remaining work

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
