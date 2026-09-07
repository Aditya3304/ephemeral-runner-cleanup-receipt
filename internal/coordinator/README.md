# Trusted local collection and finalization handoff

Milestone d coordinator contract, implemented and validated September 7, 2026.
This directory owns the host ledger, collection, observations, and their tests.
The separately authorized `cmd/observe` entry point is installed only as
`/usr/local/bin/proof-observe` in `cleanup-receipt-control-plane`. It is not part
of the runner image and never executes job-supplied commands.

## Trust and scope

The local coordinator, its private ledger root, the dedicated kind node,
containerd/CRI, and the installed helper are trusted. Jobs cannot mount or write
their files. These artifacts are **unsigned host observations**: a digest binds
bytes but does not authenticate a host ledger uploaded by an untrusted party.
The finalizer must obtain them through its operator-configured private host
directory. Signing does not turn `accepted-untrusted` job claims into trusted
workspace, credential, resource, or disposal observations.

Log coverage means the exact CRI records for container `guard`, including its
main/post wrapper and child-command stdout/stderr. `job.log` retains raw timestamps,
stream names, partial/full record tags, and payload bytes without normalization.
The collector does not claim coverage of the trusted `network-gate` init
container, node services, arbitrary job-created files, or external logs.

## Collection sequence

1. The coordinator saves the exact Pod/Job/namespace/cluster UIDs and source/run
   identity. Before releasing the existing nonce-bound network gate, it saves a
   collector request bound by SHA-256 to these fields, requested image, command,
   staging UID and cluster RBAC UIDs.
2. A detached helper validates the exact CRI sandbox identity, a running
   `network-gate` init container, and the absence of any guard attempt. It opens
   the pinned Pod log directory without following symlinks. It creates only its
   empty `guard` subdirectory, never a runtime log file, to eliminate the race
   in installing a watch on a newly created subdirectory.
3. Before the init gate can release, the helper installs kernel watches on the
   pinned directories, confirms they contain no pre-existing guard log, checks
   the runtime again, and durably publishes `armed.json`. The coordinator also
   checks that the collector is alive before accepting this marker.
4. The collector observes the first `0.log` creation, pins its descriptor, and
   copies its bytes into a private durable spool. It syncs after each bounded
   read. Rotation, replacement, unexpected files, directory loss, queue overflow,
   truncation detected by offsets/hash, and writer activity after close preserve
   an irreversible coverage gap. Rotation is conservatively incomplete even if
   some or all rotated data might still be obtainable.
5. Complete coverage requires a single guard attempt, unchanged container ID,
   all sandbox containers exited, matching CRI identity/log path, valid start/end
   chronology after arming, and an observed log-writer `CLOSE_WRITE`. A quiet
   polling interval or a kubelet log snapshot is insufficient. The helper hashes
   the entire pinned source and spool and requires byte-for-byte digest and size
   agreement after termination and final watch draining.
6. Before Pod deletion, the stopped-runner observer opens only
   `/var/lib/kubelet/pods/POD_UID/volumes/kubernetes.io~empty-dir/work` and its
   `proof-RUN/{workspace,credentials}` children. Every component is opened without
   following symlinks; scoped children additionally refuse mount crossings and
   path escapes. An absent run child within an existing pinned volume, an absent
   target directory, or an empty target directory is verified. Hidden entries
   are residual failures. Missing volume roots, unsafe symlinks, and read errors
   are unobservable. No job-supplied path or ownership marker authorizes access.
7. The host requires the helper's container ID, observed image and exit code to
   agree with its API ledger. It copies and rehashes the exact bounded log bytes,
   syncs them, and persists canonical `collector.json` before runner disposal.
   Kubernetes namespace and cluster RBAC absence are observed independently by
   the coordinator with UID preconditions and replacement protection.

Credential coverage combines independent stopped-runner credential-file absence
with confirmed runner namespace and run-scoped RBAC disposal. It does not claim
revocation of external credentials or physical storage erasure. Resource coverage
is the assigned namespace in this restricted runner profile, which cannot create
PVC/PVs. Job evidence remains a separate input, including negative claims.

## Bounds and recovery

The per-job raw log limit is 100 MiB. The collector retains at most that many
bytes; exceeding it prevents complete coverage. CRI command output is bounded
while being read, command timeouts are five seconds, watch buffers are 64 KiB,
helper JSON is bounded at 64 KiB, and the total helper lifetime is the command
timeout plus five minutes (at most 900 seconds). Host hashing streams through
bounded readers. An empty, continuously observed log is valid; missing start or
termination evidence is not equivalent to an empty log.

The node spool is `/tmp/proof-observe-RUN-POD_UID`, root-private with exclusive
creation and an advisory lock. A coordinator process death does not terminate
the detached helper. The existing `collect` command resumes without replaying
the Job. A helper death cannot re-arm an old observation window: after lock loss,
reading its result preserves its available prefix as explicitly unobservable.
Node loss or missing spool is a recoverable collection error and never authorizes
passing log coverage. Transient transfer/filesystem errors retain collection
sources. The node spool and host artifacts remain for operator-managed retention;
the 100 MiB bound is per job, not an unlimited-history disk quota. No global
kubelet, firewall, or laptop settings are modified by this helper.

Existing kubelet snapshot collection remains only for runs with no pre-start
collector request and stays `snapshot-unattested`; it cannot become verified logs.

## Finalizer contract

Files in the coordinator run directory:

| File | Meaning |
| --- | --- |
| `run.json` | Durable host ledger, including identity/source, image, UIDs, terminal status, collector binding, hashes and disposal outcomes |
| `collector.json` | Canonical `kind-node-observation/v1`, SHA-256 bound by the ledger |
| `job.log` | Exact raw CRI bytes; ledger and collector bind SHA-256 and byte size |
| `cleanup-evidence.json` | Accepted but untrusted preliminary evidence, when available |
| `guard.json` | Untrusted guard status, when available |
| `observations.json` | Canonical `local-ci-observations/v1`, binding exact ledger SHA-256, identity, artifact hashes, collector result and five derived coverage findings |
| `finalization-ready.json` | Canonical `local-ci-ready/v1`, published atomically **last**, binding identity, exact ledger SHA-256 and exact observations SHA-256 |

The readiness marker fields are `kind`, `identity`, `ledger_sha256`, and
`observations_sha256`; its limit is 4096 bytes. The observation document limit is
1 MiB. Neither input contains the marker's digest, so the binding is not circular.

**A `phase: complete` ledger alone is not finalization-ready.** A finalizer must
defer before freezing any inputs if the marker is absent, and reject mismatched
marker hashes. This includes old milestone-c runs lacking a marker. Missing job
evidence can be represented honestly only after the host publishes a committed
observation snapshot and readiness marker. A crash between ledger, observation,
and marker writes is repaired by `collect` without changing the completed ledger.
Existing observations/markers are immutable; conflicting publication is rejected.

Exported APIs in package `coordinator`:

- `ValidateEvidence(data []byte, l *Ledger) error` validates untrusted preliminary
  evidence without promoting its provenance.
- `ValidateObservations(data []byte, l *Ledger, ledgerData []byte) (*Observations,
  error)` checks canonical form, exact ledger binding, collector identity and
  derived coverage. It does not read artifact files.
- `ValidateFinalizationReady(data, ledgerData, observationsData []byte) error`
  validates the final atomic commitment against the exact snapshot bytes.
- `(*Coordinator).LoadObservations(run string) (*Observations, error)` requires
  the readiness marker and also checks exact on-disk log/evidence/guard hashes.

Finalizer policy must still enforce approved source/image/finalizer identity,
archive these exact bytes, validate evidence semantics, and obtain a real signature.
The helper and coordinator do not hold signing, archive, or database credentials.

## Validation performed

Offline tests passed for `./internal/coordinator/...`, `./cmd/observe`, and
`./cmd/localci`. They preserve the prior coordinator tests and add real inotify
create/close/rotation checks, synthetic queue-loss and replacement checks, hidden
residual/symlink/path-escape checks, timestamp format checks, missing start/end
rejection, bounded transport, exact-byte artifact verification, coverage forgery,
source/image/container/Pod mismatch, and publication crash/recovery/tampering tests.

The existing **unmodified** six-case live suite exited zero, recorded in
`.build/ci-validation-1788738603474168458.json`:

| Case | Run ID | Trusted logs | Trusted workspace/credentials |
| --- | --- | --- | --- |
| pass | `91df5b9ef7a04614794031882fd9ae7b` | verified | verified / verified |
| fail (command exit 7) | `945e39be12bfc4029904a9dfd8a51669` | verified | verified / verified |
| cancel | `849148c71944d6d787c7afb4037e0b8d` | verified | verified / verified |
| missing-post (SIGKILL) | `d7a9fae714f55f9129304fe98436c24a` | verified | failed / failed |
| coordinator restart | `99108a87e31f8837e179586cbbf48313` | verified | verified / verified |
| isolation | `b4de8b1f89101878336533f0ca8e243b` | verified | verified / verified |

All six confirmed resource/runner disposal and published readiness markers. The
restart case retained the original Job UID and exactly one job-start log record.
Missing-post independently found residual files; no unsigned job claim was
promoted to achieve the result. The passing case collected 732 bytes with SHA-256
`4d2f1ecca959c1e18ff742e45aba7cfeac664f93732d07921195f2bd974d866e`.
This is coordinator evidence, not an assertion that finalizer signing/archive
acceptance was tested by this component's suite. The final host hash reader was
subsequently changed to stream with nofollow protection and passed unit tests;
the integration owner will rerun the full suite against the frozen source.
