# Milestone i validation — local release and rehearsal

Validated on September 7, 2026, on the prepared Ubuntu/WSL laptop. This milestone
completes the approved single-laptop, offline, zero-spend release. AWS, Terraform,
hosted GitHub Actions and public Sigstore remain intentionally excluded.

## Release lifecycle

Version `0.1.0` provides one repeatable interface:

```bash
bash dev release-up
bash dev release-status
bash dev release-stop
```

The tested stop operation stopped the watchdog, API/gateway, private Sigstore,
MinIO/KES, PostgreSQL and the exact label-checked kind control-plane container.
It retained all 28 project Docker volumes and did not remove the cluster. Startup
restarted the same identities, applied no new migration, loaded no package manager,
performed no image pull, and finished with all eleven runtime containers ready.

The status verifier checked Kubernetes readiness; active/enabled watchdog and WSL
linger; the running API's exact image; the installed watchdog binary and image
policy; the preloaded runner digest; dashboard availability; and fresh signature
plus archived-artifact verification for a genuine receipt.

## Known-good PostgreSQL backup

The retained custom-format backup is 41,994 bytes with SHA-256
`84cb8d9259de9d061601b85ebb60f08baeb64fcec6ea63e1802199c870081ef2`.
It contains schema version 2 and the sanitized metadata snapshot that existed
before rehearsal:

| Table | Rows |
|---|---:|
| receipts | 33 |
| incidents | 7 |
| finalization attempts | 65 |

Creation compared metadata identities before and after `pg_dump`. Every restore
created a uniquely named disposable database, restored with `pg_restore
--exit-on-error`, and matched row counts plus SHA-256 identity digests for all
three tables and the Goose schema version. The disposable database was then
dropped. The live database was never replaced or cleared.

The backup contains no database passwords, API tokens, private signing keys, raw
job logs or MinIO objects. Those remain in their purpose-specific Docker volumes.

## Two complete rehearsals

Each rehearsal began with the entire release stopped, restarted it from prepared
local assets, restored the known-good backup, and ran one passing and one abruptly
interrupted local Kubernetes job. The separate trusted finalizer archived and
signed each receipt; delivery wrote verified metadata through the API.

| Rehearsal | Startup | Pass receipt | Interrupted receipt | Result |
|---|---:|---|---|---|
| 1 | 54.063 s | `02af0a4e-763b-4b4f-820a-40dc717139bd` | `3c8ec248-ac66-4c52-a4bb-47a94db49e87` | verified pass; verified fail with incident `711e6f68-4ca3-4849-8525-c2513e6152af` |
| 2 | 56.557 s | `3bea3887-dc73-4d55-831d-0d1c9a5c7dd3` | `f8fc1e06-e79d-44ad-9e72-4ec310483d3e` | verified pass; verified fail with incident `ea51830d-0116-4005-a71e-f7cbc6937937` |

All four runs independently verified their signature and exact archived objects.
All four resource namespaces and runner namespaces were confirmed absent. The two
interrupted runs each created exactly one incident. Both rehearsals ended with
the watchdog active, enabled and persistent, and used zero runtime downloads.

## Demo recording and sample evidence

The real Chromium recording uses the second rehearsal's pass and failure receipts.
It shows fresh verification, signed evidence references, an honest failed workspace
observation, its incident and recovery history. Every non-local browser request was
blocked. The WebM is 1,796,922 bytes with SHA-256
`175e4cc0377f6a994553c8ff8100b9cdfe964ceedf34106457f382a7fa618124`.
Two full-page screenshots are retained beside it.

The release dossier retains the passing and failing canonical receipts, genuine
Sigstore bundles, immutable archive result references, offline CLI verification
records, API metadata and fresh API verification. It does not copy archive or
signing credentials. The exact log/evidence versions remain under MinIO's tested
SSE-KMS, versioning and seven-day COMPLIANCE retention.

## Release package and final scope

The annotated Git tag is `v0.1.0`. The generated release directory and `.tar.gz`
contain the tagged source, contract and deployment hashes, pinned image/trust
identities, backup/restore proof, two rehearsal reports, sample signed evidence,
recording, screenshots and a SHA-256 inventory. The package verifier rejects
missing, added, changed, linked or path-escaping content and compares the compressed
archive byte-for-byte with the verified directory inventory.

The package intentionally does not contain private secrets or a multi-gigabyte
Docker image export. It runs on this prepared laptop, as requested. GitHub is only
the public source and tag destination; application execution is completely local.

Milestones a through i and every approved local acceptance checkbox are complete.
The original GitHub/public-Sigstore/AWS execution gates remain deferred because the
user explicitly required fully local execution and no paid resources.
