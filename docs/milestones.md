# Milestone status

The user approved the solo, offline, zero-spend build plan on September 7, 2026.
Each completed milestone is demonstrated and requires confirmation before the next.

| Gate | Status | Scope |
|---|---|---|
| Plan approval | Approved | Local Kubernetes CI, private Sigstore, local MinIO/PostgreSQL; no paid cloud resources |
| a: foundation | Complete; awaiting next-milestone confirmation | Repository, schema/migrations, TLS, restricted database roles and repeatable local commands |
| b: cleanup CLI | Not started | Cobra/client-go commands and real kind cleanup |
| c: guard/coordinator | Not started | Local job lifecycle and disabled GitHub adapter |
| d: signing/finalizer | Not started | Local identity/Sigstore, trusted archival, canonical receipt |
| e: API | Not started | Chi/pgx verification, sanitization, transactional ingestion and queries |
| f: dashboard | Not started | React/Vite/Tailwind/TanStack search/detail/incident UI |
| g: watchdog | Not started | Missing-finalization recovery and incidents |
| h: hardening | Not started | End-to-end fault and offline-execution matrix |
| i: local deployment | Not started | Packaging, backup and rehearsals; AWS remains excluded |

## Explicit milestone a deferrals

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
