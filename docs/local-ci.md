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
6. Before releasing the init gate, the coordinator arms a detached trusted
   collector in the fixed kind node. It watches the pinned guard CRI log directory
   from before first file creation through observed runtime termination and log
   writer close. It persists at most 100 MiB of exact raw CRI records outside the
   runner; rotation, truncation, replacement, watch loss, missing start/end proof,
   and limits prevent complete coverage. The helper survives coordinator restart.
7. After the guard stops and before Pod deletion, the helper independently checks
   the pinned emptyDir workspace and credential directories without following
   symlinks or accepting job-supplied paths. Hidden residual files are failures;
   unavailable volume roots and unsafe paths are unobservable. The coordinator
   separately verifies namespace and run-scoped RBAC disposal. Accepted job
   evidence remains **untrusted**. Transient collection errors retain their source
   for a later `collect`; privileged deletion uses only the host ledger's UIDs.

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
`cleanup-evidence.json`, `guard.json`, `collector.json` and `job.log`. These files
stay out of Git. The ledger records run/source/image identity, cluster/resource/
runner/Job/Pod/container identities, observed termination, cancellation, exact
artifact hashes, missing/rejected evidence and recovery errors. Raw CRI `job.log`
records include timestamps, stdout/stderr stream names, record tags and payloads;
they cover the guard and its child command, not node services or init diagnostics.
The staging area is mutable and untrusted; these local files are not an immutable
S3-compatible archive.

The host publishes canonical `observations.json` with five independent coverage
results and the SHA-256 of the exact completed ledger. It then atomically publishes
`finalization-ready.json` **last**, binding both ledger and observation hashes.
`phase: complete` alone is insufficient for finalization: a finalizer must defer
without freezing inputs until a valid readiness marker exists. `collect` repairs
interrupted publication without changing the completed ledger; conflicting
snapshots are rejected. Old runs lacking a readiness marker cannot be finalized
automatically.

Preliminary evidence stays unsigned and partial (or failed). The collector's
observations are unsigned **trusted host** data, accepted only from the private
operator-configured ledger directory. Signing a job claim does not upgrade its
observer. Fully observed raw logs use ledger status `complete`; legacy snapshots
remain `snapshot-unattested`. Credential coverage combines stopped-runner file
absence with independently confirmed runner namespace and RBAC disposal. It does
not assert external credential revocation or physical storage erasure.

The observer is built from `cmd/observe` and installed at
`/usr/local/bin/proof-observe` only in `cleanup-receipt-control-plane`. It changes
no global kubelet, firewall, or laptop settings. Its lifetime is bounded at fifteen
minutes and its log spool at 100 MiB per run. Node-spool loss remains incomplete;
the per-run bound does not replace operator-managed retention of historical runs.

The unchanged live six-case regression passed with this collector. Pass, failed
test command, graceful cancellation, coordinator restart and isolation all had
independent verified coverage. The SIGKILL/missing-post case retained complete
logs but independently detected residual workspace and credential files, requiring
a failed cleanup outcome. Every case published a valid readiness marker. See the
[coordinator contract and validation record](../internal/coordinator/README.md)
for run IDs, fault tests and exact limits. Finalizer keyless signing and immutable
archival are milestone d's separate integration work; API ingestion and dashboard
visibility remain later milestones. No receipts are inserted into PostgreSQL by
the coordinator.

The disabled GitHub workflow and adapter are documented in [guard.md](guard.md).
Local lifecycle tests do not validate GitHub's post-step or cancellation behavior.
