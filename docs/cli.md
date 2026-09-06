# proofctl — milestone b

Run in Ubuntu/WSL (Linux). PostgreSQL from milestone a can keep running; these CLI
commands do not write to it.

```bash
bash dev cli-prepare   # One-time downloads of pinned Go/tool/image dependencies
bash dev kind-up       # Dedicated cleanup-receipt cluster; preserves default kubeconfig
bash dev cli-check     # Filesystem, ownership, canonical JSON and offline Cosign tests
bash dev kind-check    # Real Kubernetes resource/volume, timeout and safety tests
bash dev cli-demo      # Actual CLI lifecycle; saves preliminary evidence under .build/demo
```

After setup, builds/tests use cached dependencies. `kind-up` requires a preloaded
node image. Runtime commands do not need public Sigstore or GitHub. This milestone
tests offline bundle verification; the full network-blocked application demo and
private signing services belong to later milestones.

## Commands

`begin` takes a new state-file path, repository/run/attempt/job/source revision,
Kubernetes configuration and sandbox parent. It generates a random ownership
token, creates `proof-<token>` namespace and filesystem sandbox, then records their
identities. It refuses to reuse an existing context or adopt an existing directory.

```bash
.build/proofctl --kubeconfig .build/kubeconfig --state .build/example-context.json begin \
  --sandbox-root /tmp/proofctl-runs --repository owner/repository \
  --run example-1 --attempt 1 --job test --revision FULL_GIT_COMMIT_HASH
.build/proofctl --kubeconfig .build/kubeconfig --state .build/example-context.json inventory
.build/proofctl --kubeconfig .build/kubeconfig --state .build/example-context.json cleanup --timeout 120s
.build/proofctl --state .build/example-context.json receipt --out .build/example-evidence.json
```

Replace the revision placeholder with the actual 40- or 64-character lowercase
source commit. State and CLI binaries must stay outside the disposable workspace.
Each command locks the state file and saves updates atomically. State is an
untrusted job observation, not finalizer authorization or cryptographic proof.

`inventory` discovers listable namespaced APIs and records metadata/UIDs only;
secret contents are never collected. Cluster-scoped cleanup is restricted to
explicitly owned PersistentVolumes. PVs require the run label and, when bound,
a claim reference matching a recorded PVC UID in the owned namespace. Relevant
unlabelled PVs prevent a successful cleanup observation instead of being skipped.

`cleanup` wipes the owned workspace and credential directory using pinned Linux
directory descriptors. It does not follow symlinks, overwrite hardlinked file
contents, traverse mounts, change permissions, or force finalizers. Directory
replacements and ownership-marker changes are rejected. For bounded execution,
each directory tree is limited to 10,000 entries and 128 nesting levels; exceeding
either limit produces an explicit failure with residual paths left visible.

Kubernetes cleanup pins the cluster and namespace UID, checks the run label and
inventoried object identities, then terminates the namespace. Kubernetes removes
its scoped objects. The CLI independently checks those objects for absence and
removes only matching tracked PVs after their claims disappear. A final volume
scan catches relevant volumes that appeared during cleanup. Unknown APIs, denied
inventory, changed UIDs, stuck finalizers and timeouts prevent success. No broad
namespace/cluster deletion or finalizer-stripping fallback exists.

`receipt` emits RFC 8785 canonical JSON with independent workspace, credentials,
resources, logs and runner-disposal coverage. It is **unsigned preliminary
evidence**, not the canonical signed final receipt. Observations here are marked
`job`; unobservable logs/disposal keep the verdict partial. A recorded cleanup
failure stays visible after retry. Existing evidence files are never overwritten.

`verify` checks the exact artifact bytes against an expected SHA-256 and invokes
pinned Cosign for offline certificate/identity/transparency verification:

```bash
.build/proofctl verify --artifact receipt.json --bundle receipt.sigstore.json \
  --trust-root operator-trusted-root.json --sha256 EXPECTED_SHA256 \
  --identity EXACT_SIGNER_IDENTITY --issuer EXACT_OIDC_ISSUER
```

It snapshots bounded regular-file inputs before verification, requires separately
provisioned operator trust, clears ambient Sigstore settings, and exposes no
skip-verification, static-key or identity-regex shortcut. A valid blob signature
does not assert the truth of a cleanup verdict. The included upstream Sigstore
test fixtures are not this project's receipts or finalizer identity.

## Version pins and remaining boundaries

- Go 1.27.1, Cobra 1.10.2, Kubernetes/client-go 0.36.1.
- Project-local kind 0.32.0 and Kubernetes node 1.36.1, image pinned by digest.
- Project-local Cosign 3.1.3. Both tool binaries are checked against SHA-256 digests
  supplied by the official GitHub release metadata during setup; installed global
  tools are untouched.
- The kind cluster is a local developer test environment, not yet a hardened CI
  runner boundary. RBAC/admission/network policies for hostile jobs are milestone c.
- HostPath test PV/PVC objects are real and bound. Their removal establishes
  Kubernetes object absence, not forensic erasure of underlying storage blocks.
- Full log archival, runner destruction observed from outside the runner, private
  OIDC/Fulcio/Rekor signing, final receipt creation and API ingestion remain pending.
- GitHub-hosted and AWS integration remain deferred under the approved offline scope.

The application must later trust the coordinator/finalizer's independently
validated observations, not fields a PR job can edit in its context file.
