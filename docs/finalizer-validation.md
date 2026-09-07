# Milestone d validation

Completed September 7, 2026, on this laptop in WSL Ubuntu. Six actual local jobs
were independently observed, archived into protected MinIO object versions, signed
through private Sigstore, and verified with the cleanup CLI. No hosted workflow,
public signing service, paid service, or application database write was used.

## Actual end-to-end results

| Scenario | Run ID | Command exit | Signed cleanup verdict |
|---|---|---:|---|
| Passing command | `2687c9f75993ac1041d1a8b4e89ded21` | 0 | pass |
| Failing command | `a4472ce11ddaf0df6cf9af5a8b0fc4dd` | 7 | pass |
| Graceful cancellation | `d126f06e244f6f58c1154f80a7990427` | 143 | pass |
| Abrupt termination; missing post | `c0a68b55e36622db63c2c3ff24ca9f9e` | 137 | fail |
| Coordinator restart | `e80b6858da20377c88d4c0c0a9ae4ba3` | 0 | pass |
| Runner isolation | `1ada8b5a7cdf32ff609a5bc9b2600bba` | 0 | pass |

Every signature verified. A failed test command does not imply cleanup failed:
the failing-command case preserved exit 7 while independently proving cleanup.
The abruptly killed runner left workspace and credential files; the trusted node
observer detected them before disposal. Its signed failure receipt honestly records
missing preliminary evidence, complete logs, and the observed residual failures.
All six resource/runner namespaces were subsequently confirmed absent. Coordinator
error lists were empty. Restart recovery retained the original Job UID and exactly
one job-start marker.

The suite exited zero and saved `.build/finalizer-validation-1788740125296465069.json`.
Its lifecycle results are `.build/ci-validation-1788739988836255612.json`.
`milestone-d-results.json` beside the repository is a portable copy of the final
result table with complete immutable references and coverage observations.

## Signature and archive verification

Each receipt was checked in a fresh container with **networking disabled**, an
explicit independently installed trust root, and exact issuer/signer requirements:

- The real receipt and keyless bundle verified.
- Changed receipt bytes were rejected even when the supplied digest was recomputed
  to match the altered bytes, exercising actual signature verification.
- A wrong signer, wrong issuer, and unrelated trust root were rejected.

These are six positive and twenty-four negative signature checks. Separately,
read-only archive credentials downloaded all referenced evidence, observations,
ledgers, logs, receipts and bundles from their exact versions. All **35 downloads**
matched their recorded sizes and SHA-256 digests and passed encryption/retention
metadata checks. That verifier had no upload or signing credentials and joined
only the archive network. Per-run `verification.json` records are retained under
`.build/finalized/`.

Repeating finalization of the first completed run returned the same receipt and
bundle bytes, SHA-256 values and object version IDs. It did not issue a new receipt
or replace the archived versions.

A separate real guarded job, `b371ccc861f851dceaf2cc06bf2b12c8`, could use its
authorized Kubernetes API but could not connect to any of seven live issuer,
Fulcio, CT, Rekor and timestamp TCP listeners. All seven listeners were reachable
from an authorized signing-network client before that test. The signing services
published no host ports, the runner had no signing keys or Docker socket, and its
cleanup completed. `scripts/sigstore-isolation.py` saves the raw result in
`.build/sigstore/service-isolation.json`.

## Reproducible identities and sample artifacts

- Job source and embedded finalizer revision:
  `b42fd6d4496336233d9d490531ca2a6265932505`.
- Finalizer image: `sha256:9523cabac27a326d43914aae13326b5337f9e994f0926dace901785a22c0bf3c`.
- Runner image: `cleanup-receipt/runner@sha256:e6de49a767a40a37a6af8d23c81f057f4d8ed205618dbb93586329f111742dd5`.
- Operator trusted-root SHA-256:
  `fbbd3e48e79bc80e3dad572fb9865289a0654a7de4a67edca09fdd79369afb70`.
- Passing receipt SHA-256:
  `cceb32a3398504c239f0ef8e4cdd27d887cd4868b6a08c40b77bc58f53ac186b`.
- Passing bundle SHA-256:
  `58313aac4b4ef9a27753bab82736e2f87bff744a9fd940455626cc98d4cb5026`.
- Signed failure receipt SHA-256:
  `da60504a577b5625d4f3a43591403cb033a840629e07a1a35980361f7dd620c1`.

The finalizer build rejects uncommitted implementation inputs and embeds its source
revision. The runtime image contains only approved static binaries. The tested
deployment's temporary-filesystem options are a single quoted Compose value;
the finalizer runs non-root with a read-only root, dropped capabilities, bounded
memory, private temporary storage, and no Docker socket, kubeconfig, database
credentials or job source checkout.

Portable artifacts beside the repository: `signed-cleanup-receipt.json`,
`signed-cleanup-receipt.bundle.json`, `signed-cleanup-result.json`,
`signed-cleanup-verification.json`, `local-sigstore-trusted-root.json`, and the
signed-failure receipt/bundle. Canonical signed files must not be reformatted:
changing even whitespace changes the signed bytes. The public trust root contains
verification material; private issuer, CA, archive and KMS keys remain in their
designated Docker volumes.

## Supporting checks

The private stack uses digest-pinned Fulcio 1.8.5, TesseraCT POSIX 0.1.1, Rekor
POSIX 2.2.1, timestamp server 2.0.6, and Cosign 3.1.3. Fresh ephemeral artifact keys,
short-lived identity certificates, CT evidence, Rekor inclusion proofs/checkpoints,
and RFC3161 timestamps are real. The local issuer requires the finalizer's verified
client certificate and issues fixed-identity 120-second RS256 tokens. Unauthorized
identity requests and token tampering are rejected. All signing services survived
restart with their keys, trust and log history intact. Exact pins and upstream
sources are in [the private signing guide](../infra/sigstore/README.md).

Real MinIO/KES tests verified TLS, SSE-KMS, versioning, seven-day COMPLIANCE,
conditional-upload replay, exact-version overwrite/deletion rejection (including
administrator deletion), out-of-scope operations, and retention shortening denial.
New admission renews old referenced versions without changing their IDs. A
simulated eight-day-later test verifies that behavior against a real object; it
does not claim eight days elapsed during testing. Both SDK and direct signed HTTP
unversioned verifier reads were denied. UID 65532 upload and independent read-only
verification passed, as did restart/decryption persistence and blocked external
TCP access. Details are in [the archive guide](archive.md).

Go tests and repository-wide Go vet passed. Regression tests cover trusted log
start/end and filesystem observation, watch gaps/rotation, changed ownership and
artifact identities, strict receipt/schema limits, missing evidence, tampering,
atomic-last readiness publication, conflicting replay, failed archive/signing,
sixth-attempt crash reconciliation, and renewed retention before resumed first
publication. Historical completed verification remains read-only. The original
cleanup CLI tests and genuine upstream offline verification fixture tests also
passed after the dependency update. Unit test doubles are not the evidence for
the live signature/storage claims above.

## Run and remaining scope

On this prepared laptop, start Docker Desktop and open `Run-signed-cleanup-demo.cmd`
beside the repository. In WSL, equivalent commands are `bash dev ci-up`,
`bash dev finalizer-up`, then `bash dev finalizer-demo`. No setup downloads occur
in this path. To repeat this six-case suite, use:

```sh
python3 scripts/demo-finalizer.py pass fail cancel missing-post restart isolation
```

The approved local profile still replaces hosted GitHub, public Sigstore and AWS
with Kubernetes Jobs, private identity/signing services and MinIO. Rekor's POSIX
implementation avoids a separate Trillian/MySQL deployment; the local timestamp
service supplies the timestamp evidence required by this selected upstream stack.
The entire tested signing/archive path uses private local networks; verification
additionally has no network. Full application acceptance including API/dashboard
is still pending.

Coverage establishes filesystem absence in stopped, scoped runner directories,
run-scoped namespace/RBAC disposal, and complete observed guard/child CRI logs.
It does not prove SSD block erasure, external cloud credential revocation, logs
from node services, or resilience against the laptop administrator destroying the
disk. Log rotation, watch loss, node loss or missing observations prevent a pass.
The single-node runner profile cannot provision PVC/PV resources; the standalone
CLI's volume-cleanup capability remains separate. Historical per-run spools and
retained archive objects need operator-managed capacity planning.

**Still to implement:** milestone e's verifying API and transactional metadata /
incident ingestion; f's React dashboard; g's watchdog; h's complete system fault
matrix; and i's local packaging, backups and rehearsal. No application receipts
have been inserted into PostgreSQL and no working dashboard is claimed. User
confirmation is required before milestone e.

Final health inspection found only the five baseline cluster namespaces, no PVs,
all private signing/storage services running, and healthy PostgreSQL with verified
TLS and schema version 1. Stored receipts, incidents and finalization attempts
were all zero, as expected before the ingestion API milestone. Persistent volumes
and existing user data were retained.
