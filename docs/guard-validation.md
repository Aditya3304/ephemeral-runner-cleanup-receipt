# Milestone c validation

Completed September 7, 2026 (Asia/Kolkata). The six-case live suite exited zero
against committed source `b3b8d4b90a204551de05b0ddfa02bd4297ff4db7` on this laptop's
WSL Ubuntu and dedicated kind cluster. This milestone implements automatic job
cleanup and the local coordinator. It does not yet produce signed final receipts.

## Tested artifacts

- Runner image: `cleanup-receipt/runner@sha256:6323a5c98a1c2655495ad5b4eb72ea06eef01ab8a41e63b9431cbeac86d742e8`.
- Node 24.20.0, Go 1.27.1, Kubernetes 1.36.1; pinned dependencies and image inputs
  are recorded in the repository. The runner image was built without network
  access and preloaded into kind.
- Raw local suite result: `.build/ci-validation-1788735441493805739.json`.
  A portable copy is supplied beside the repository as `milestone-c-results.json`.
- Passing-run evidence SHA-256:
  `cb9275575e54c551b5b9ab92273c72806019fad3414a83d4e7933de4334fc773`.
  Copies of the evidence and its coordinator record are supplied beside the
  repository as `cleanup-evidence-milestone-c.json` and `milestone-c-sample-run.json`.
  These local artifacts are not signed or archived immutably.

## Actual local lifecycle results

| Scenario | Run ID | Runner exit | Result |
|---|---|---:|---|
| Passing command | `cdd2f6ca308d6f0b64152eb2d0b32155` | 0 | Post completed, cleanup exit 0, evidence accepted as untrusted |
| Failing command | `ca0117eed2b6eb72c06646fe74c37ff4` | 7 | Original failure preserved; post completed and cleanup exit 0 |
| Cancellation | `521798992b2e5daa6a7ee1071cb94611` | 143 | SIGTERM observed; command did not time out; post completed and cleanup exit 0 |
| Abrupt runner termination | `b9a80aff60279e5422ef0daf82693f55` | 137 | Post and evidence honestly marked missing; coordinator disposed of remaining resources |
| Coordinator restart | `7dcd35350dbf9321f3998c69c35b7fa9` | 0 | Same Job UID recovered; log contains exactly one job-start marker; cleanup exit 0 |
| Isolation | `b8e8ad6de068486c46a627652881f21d` | 0 | Allowed API access worked; forbidden network/resource operations were blocked; cleanup exit 0 |

The coordinator independently confirmed the resource namespace and runner
namespace absent in every case. No forced removal of resource finalizers was used.
The cancellation record retains one retry diagnostic:
`cancel-signal-retry: unable to upgrade connection: container not found ("guard")`.
A repeated signal attempt raced with container exit. The saved guard status shows
SIGTERM cancellation, no command timeout, successful post and cleanup, and empty
guard/staging errors. The other five coordinator records have empty error lists.
The raw report preserves this diagnostic; it has not been edited away.

The isolation test used positive controls: an ordinary control pod could reach
the protected pod and host TCP/UDP canaries, while the actual guarded job could
not. Tested host ports included 18080 and 31080; outbound 1.1.1.1:443 was blocked.
Kubernetes API access required by cleanup remained available. Forbidden API
operations and protected staging metadata edits were rejected, immutable inputs
were checked, and an administrative server-side dry-run of a privileged host-PID
pod in the resource namespace was rejected by admission policy.

## Supporting validation

- All 21 Node guard tests passed on Linux Node 24, including signal handling,
  timeout, cleanup failure, missing/repeated post, identity/transport validation
  and loading the bundled distribution without `node_modules`.
- Go proof/coordinator/netgate tests passed, covering assignment and filesystem
  safety, durable recovery, ambiguous launch outcomes, evidence validation,
  transient collection failures, independent disposal, gate ownership and the
  fresh-init nonce handshake. Go vet passed for the repository.
- The original four live CLI cleanup groups passed again: real PV/PVC and
  namespace/service-account/filesystem cleanup with an outside sentinel;
  bounded handling of a held finalizer; changed namespace ownership; and a
  same-name resource with a different UID.
- Genuine upstream offline Cosign verification fixtures and their negative cases
  passed. These test verification support, not the still-pending local signer.
- Final cluster inspection found only its five baseline namespaces and no PVs.
  PostgreSQL remained healthy. Persistent database volumes were retained.

## Boundaries and remaining work

The local Go coordinator and Kubernetes Jobs are the approved replacement for
hosted GitHub orchestration. The Node 24 GitHub main/post adapter is implemented
with bundled official Actions libraries, but its example workflow is disabled.
Actual GitHub cancellation, artifact upload and post-step semantics are untested.

Jobs use a restricted, single-node local profile with no PVC/PV provisioning.
Standalone CLI volume cleanup was tested separately above. NetworkPolicy is
supplemented by a trusted, verified per-pod network filter because the controller
alone allows host NodePort/ICMP exceptions. The filter is installed inside the
runner's network namespace before the fresh-init gate opens; it does not change
the laptop's global firewall. This is the tested local IPv4/amd64 profile, not a
claim of arbitrary cluster or hostile-kernel isolation.

Evidence accepted from a job remains untrusted and unsigned, with partial/failed
coverage. Log collection is currently `snapshot-unattested`; it does not claim
complete or immutable archival. No application receipt has been cryptographically
verified and committed to PostgreSQL, and there is no working dashboard yet.

Remaining milestones: **d** private OIDC/Fulcio/CT/Rekor, MinIO and separate trusted
finalizer; **e** verifying API; **f** dashboard; **g** watchdog; **h** full end-to-end
fault and offline matrix; **i** local packaging, backup and rehearsal. Real hosted
GitHub, public Sigstore and AWS acceptance gates remain explicitly deferred under
the approved local/offline, zero-spend scope. The complete network-blocked path
through signing, storage, ingestion and dashboard has not yet been implemented.

## Repeat the demonstration

On this prepared laptop, start Docker Desktop and use `Run-local-CI-demo.cmd`
beside the repository, or run `bash dev ci-up` followed by `bash dev ci-demo` in
WSL. This runs one passing and one deliberately failing job, both with cleanup.
Use `bash dev ci-check` for the six-case lifecycle suite. Cached startup and demo
do not require dependency downloads. A fresh installation first needs the
documented `bash dev ci-prepare` setup downloads.

Milestone c is complete. User confirmation is required before milestone d.
