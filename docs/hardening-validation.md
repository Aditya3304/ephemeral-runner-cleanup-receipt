# Milestone h validation — hardening and fault matrix

Validated on September 7, 2026, on the prepared Ubuntu/WSL laptop. All sixteen
hardening groups passed using local Kubernetes, PostgreSQL, MinIO, private
Sigstore, the API, watchdog, and dashboard. No paid service was used.

## What the complete matrix proved

| Area | Real behavior observed |
|---|---|
| Job lifecycle | Fresh success, command failure, cancellation, skipped post, coordinator restart, and isolation runs all reached signed, archived, independently verified receipts. A failed command still cleaned successfully; skipped post produced a failure and one incident. |
| Cleanup safety | Assigned namespaces, service accounts, RBAC, runner namespaces, and tracked resources were absent. Deliberate residue and UID ownership mismatches prevented a passing claim. An unrelated namespace and host canary survived. |
| Evidence and logs | Complete collector logs matched the signed digest. Missing or interrupted collection stayed explicit and could not become `pass`. Evidence identity, schema, size, run attempt, job binding, canonical form, and names were enforced. |
| Trust boundary | Runner checks could not reach database, archive, signer, private Sigstore, host, Docker, kubeconfig, or public-network targets. The trusted finalizer used its fixed image and never executed job-controlled evidence. |
| Signatures and ingestion | Genuine private keyless signatures verified. Tampered receipt/evidence/bundle data, wrong issuer, wrong signer, and wrong policy were rejected before insertion. Twelve concurrent submissions produced one receipt and one incident; forced rollback left neither. |
| Storage | Archive TLS, SSE-KMS encryption, versioning, and seven-day COMPLIANCE retention were checked. Finalizer, verifier, and service credentials could not overwrite or delete protected versions. |
| Recovery | Real MinIO, signing issuer, PostgreSQL, archive-read, and Kubernetes-node outages were injected. Retry deadlines survived, early replay spent no attempt, preserved output was reused, and recovery created no duplicate receipt. |
| Dashboard | Seventeen Chromium checks passed for real verification, incidents, filters, deep links, error and empty states, history, dark mode, keyboard use, accessibility, 320 px reflow, mobile layout, and reduced motion. |

The six fresh lifecycle receipts were:

- success `ed7f41c3-dbff-44af-a81c-8c7e45b3bb6e` — `pass`;
- failed command `da4b7056-d0e0-4402-bcfa-4e5fa0b96c3a` — cleanup `pass`;
- cancellation `f344decf-ddaa-4ec0-b72e-167962e9076c` — cleanup `pass`;
- missing post `0524579f-d6c0-42b3-8527-cd7181b61d29` — `fail`, one incident;
- coordinator restart `b9c7455c-eaf1-4ba9-9fb5-b8203293c94b` — `pass`;
- isolation `9ee78fd6-687e-408e-8b98-af5b65a67d7c` — `pass`.

Each signature and every referenced archived object passed the API's independent
verification. Resource and runner namespaces were absent for all six.

## Fault found and repaired during validation

Stopping the kind control-plane node destroyed the detached collector's private
temporary directory. The original coordinator treated that permanent loss as a
temporary dependency failure, so it could keep retrying and never dispose of the
runner. The matrix caught this on run
`1f5b4069a6fd4a0cf2b21457a5767a46`.

Commit `235ab0a882950588bba3a4d3efd754d0a402d52e` added a narrow terminal signal:
the node observer returns exit 66 only when an already-armed result is
irreversibly gone. Ordinary API, filesystem, and readiness failures remain
retryable. The coordinator records missing logs, continues UID-bound disposal,
and allows the finalizer to issue an honest non-passing receipt.

The repaired fresh run `eabcfbc830b8576048a79c680690d754` behaved as follows:

- attempt 1, while Kubernetes was unavailable: `retry` at `collect`;
- early replay after restoration: no attempt consumed;
- attempt 2: same pinned container, restart count zero, command run once;
- result: signed and independently verified `partial` receipt
  `73539d89-bf3e-4c5f-b011-2eea2af0ef3b`;
- exactly one incident `0b2a56c4-c97d-483e-b760-75ba7442006c`;
- logs explicitly `missing`; both assigned namespaces confirmed absent.

The originally interrupted run was also safely recovered as partial receipt
`0996e37f-bb4c-4868-98bb-f0211684b69a`, preserving its prior retry history.

## Service outage results

| Fault | Run | Failed stage | Recovery |
|---|---|---|---|
| MinIO unavailable | `c56da1430c0bb82acb82b773fad8df40` | archive | attempt 2, verified pass |
| local signing issuer unavailable | `ec155b19d236e70456a078521ad0e8fe` | sign | attempt 2, verified pass |
| PostgreSQL unavailable | `41778bb8cc1e31a3d574d19ec169cfb3` | ingest | signed objects preserved; attempt 2, verified pass |
| archived-object read unavailable | receipt `9ee78fd6-687e-408e-8b98-af5b65a67d7c` | API verification | HTTP 503 while down; exact verification after restart; metadata unchanged |

## Retained evidence and scope

The portable machine-readable result is `milestone-h-results.json` beside the
repository. The live ledger now contains 33 genuine verified receipts: 26 pass,
three fail, and four partial. Seven non-passing receipts have seven incidents.

The trusted finalizer remains revision
`b42fd6d4496336233d9d490531ca2a6265932505`. Trust-root SHA-256 is
`fbbd3e48e79bc80e3dad572fb9865289a0654a7de4a67edca09fdd79369afb70`;
Cosign SHA-256 is
`4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71`.

At this gate, milestone i remained: local startup packaging, a tagged release, two
rehearsals, a known-good PostgreSQL metadata backup and restore, sample evidence
retention, and a demo recording. That work has since completed. Hosted GitHub
Actions, public Sigstore, AWS, and Terraform deployment remain excluded by the
approved local, offline, zero-spend scope.
