# Local CI coordinator and guard

This milestone runs actual Kubernetes Jobs on this laptop. GitHub is used for
source publication only. The guard and coordinator do not hold database, archive,
or signing credentials. Signing and immutable archival remain milestone d.

## Run it in Ubuntu / WSL

```bash
bash dev ci-prepare     # Initial setup downloads; caches tools and immutable images
bash dev ci-up          # Local cluster and network policy controller
bash dev guard-check   # Guard lifecycle and coordinator contract tests
bash dev ci-demo       # Real passing and failing jobs, both with cleanup
bash dev ci-check      # Cancellation, killed runner, restart and isolation checks too
```

The CLI tools from milestone b must already be prepared (`bash dev cli-prepare`).
Docker Desktop must be running. Commands after preparation use cached assets;
runner Pods use imagePullPolicy Never and reference the loaded manifest digest.
The guarded workload is intentionally limited to one active local run at a time.

To run a command from the preloaded runner image:

```bash
.build/localci run --revision "$(git rev-parse HEAD)" --job example --timeout 90s -- \
  /usr/local/bin/node /opt/examples/local-job.mjs pass
.build/localci inspect RUN_ID
.build/localci cancel RUN_ID
.build/localci collect RUN_ID
```

`run` executes the exact argument array, without an implicit host shell. The
example writes disposable files and creates a ConfigMap. A nonzero test exit
remains distinct from cleanup failure. `cancel` persists a request outside the
runner; the active coordinator forwards termination, waits for bounded post
cleanup, and collects the result before disposal. `collect` resumes an interrupted
coordinator from its ledger. The scheduled watchdog is still milestone g.

The image contains Node 24.20.0, the compiled guard and proofctl, and local example
workloads. It does not mount the host repository, Docker socket, kubeconfig,
database volumes or user credentials. Add code to an explicitly reviewed image
and rebuild with `bash dev ci-build` to run another offline workload. No checkout
or package installation occurs inside the runner.

## Lifecycle and authority

1. The coordinator records an immutable local run identity and source revision
   before creating resources. A durable launch-attempt marker prevents automatic
   command replay after an uncertain Job creation.
2. It creates separate resource and runner namespaces, recording their UIDs. Only
   the coordinator can create or dispose runner Jobs, Pods and credentials.
3. The guard receives a read-only assignment with exact namespace and cluster
   identities. `proofctl begin --assignment` validates them before creating its
   disposable workspace. The original standalone begin mode still works.
4. Guard main and post are shared by the Node 24 Action adapter and local wrapper.
   Post attempts cleanup and evidence generation after successful, failed or
   gracefully canceled commands. Process groups are stopped before workspace
   cleanup. Abrupt termination can prevent post, which stays explicitly missing.
5. A Pod-bound, expiring service-account token can patch only its pre-created
   staging ConfigMap in the runner namespace. The coordinator retrieves bounded
   JSON and validates canonical form, identity, attempt, namespace UID, chronology,
   permitted references and job-only provenance. A conflicting replay is rejected.
6. The coordinator persists accepted **untrusted** evidence and a bounded log
   snapshot before deleting the runner. Transient collection errors retain their
   source for a later `collect`. Privileged deletion uses the ledger's UIDs, never
   namespace names or references supplied in staged evidence.

The resource namespace is garbage-collected before the runner namespace. A scoped
guard verifies namespace absence because its namespaced read permissions disappear
along with its RoleBinding; it cannot continue reading individual objects after
that. The coordinator independently checks resource/runner namespace absence.
It does not force-remove resource finalizers. Independent runner and credential
disposal is still attempted when a resource remains stuck.

## Local boundaries

- The runner is non-root, has no Linux capabilities, cannot escalate privileges,
  uses the runtime seccomp profile, and has a read-only container filesystem.
  Its only writable storage is size-limited disposable volumes.
- Both namespaces enforce Kubernetes restricted Pod admission. Job credentials
  cannot create Pods, PVCs, RBAC, services, tokens or network policies. They can
  create disposable ConfigMaps, Secrets and service accounts only in the assigned
  resource namespace. Resource quotas bound object counts.
- The guard can read/list resources in its assigned namespace and read PV metadata
  for detection, but cannot create or delete cluster volumes. Live volume cleanup
  remains covered by milestone b's standalone tests; this runner profile forbids
  volume provisioning.
- The network policy controller is trusted infrastructure running with host
  firewall privileges. The local runner has no such privileges. Policy rules and
  actual traffic tests are required; merely creating a NetworkPolicy is insufficient.
- An immutable init container holds the user command until the coordinator installs
  additional input/output rules inside that Pod's pinned network namespace. These
  rules address kube-router's host-port/ICMP exceptions and policy startup races.
  The helper validates the runtime sandbox's Pod UID and pins its namespace file
  descriptor before applying rules. It does not alter the node or laptop firewall.
  Every init invocation sends a fresh random nonce and accepts only a gate release
  matching that nonce, so a recreated sandbox cannot reuse an earlier release.
- A validating admission policy allows staging data updates while preventing the
  guard from changing ownership labels, owner references or finalizers, or making
  the staging objects immutable. The coordinator's release marker is read-only to jobs.
- This profile supports the dedicated single-node, Linux/amd64 kind cluster. It
  is container isolation on a shared laptop kernel, not a separate-machine boundary.

## Inspect the evidence

Each run has `.build/coordinator/RUN_ID/run.json` plus any collected
`cleanup-evidence.json`, `guard.json` and `job.log`. These files stay out of Git.
The ledger records Pod UID, image identity, observed termination, cancellation,
digests, missing/rejected evidence and recovery errors. The staging area is mutable
and untrusted; it is not the future immutable S3-compatible archive.

`phase: complete` means coordinator collection/disposal finished. It does not mean
the test passed or that a signed cleanup receipt exists. Preliminary evidence
stays unsigned and partial (or failed); log snapshots are `snapshot-unattested`.
Complete log coverage, signing, finalizer identity, API ingestion and dashboard
visibility are later milestones. No receipts are inserted into PostgreSQL here.

The disabled GitHub workflow and adapter are documented in [guard.md](guard.md).
Local lifecycle tests do not validate GitHub's post-step or cancellation behavior.
