# Technical panel preparation: understand the implemented system

Read this for the reasoning. Use [LIVE-DEMO-WALKTHROUGH.md](LIVE-DEMO-WALKTHROUGH.md)
for exact terminal commands. [CONTROLLED-EXPERIMENTS.md](CONTROLLED-EXPERIMENTS.md)
links the real measured executions. This explanation describes the AWS profile
at `9605c536dd49faeb3acf6a80a60ec1136ad604c0`; older repository documents also
describe a separate local MinIO profile and historical local tests.

## Your opening explanation

“Our project produces signed, independently verifiable evidence of defined
cleanup conditions for ephemeral Kubernetes CI runners. A job attempts cleanup;
an observer outside the job inspects the stopped runner; a coordinator verifies
resource identity and disposal; a separate finalizer signs the resulting record.
The API verifies the signature and archived evidence before accepting it.”

“This demonstration uses real GitHub Actions runners on kind inside AWS EC2,
private Sigstore services, S3/KMS evidence storage and PostgreSQL metadata. We
use synthetic application data to make both successful and failed cases repeatable.”

## The problem, and exactly what you solve

A workflow finishing is not evidence that its temporary workspace was cleaned.
A script printing “deleted” is only a claim made by that script. A Kubernetes
pod disappearing does not establish what its filesystem contained before deletion.
A saved dashboard row can be edited unless a verifier checks the underlying proof.

Your implementation records a bounded set of observations, associates them with
the exact execution, archives their bytes and signs a receipt. It addresses
evidence integrity, execution binding and explicit reporting of cleanup failure
or incomplete observation. It does not establish universal deletion of every
copy of data anywhere in the system.

The operational order matters: observe the stopped runner before disposing of
its pod/volume. Later disposal must not hide an earlier residue finding.

## Vocabulary you should be able to explain

| Term | Meaning here |
| --- | --- |
| CI | Automated execution of checks against a repository revision |
| Runner | The agent that receives and executes a GitHub job |
| Ephemeral | Registered for one job and then disposed of; not a permanently reused runner |
| Container | A process with restricted filesystem, identity, network and resource views; shares the host kernel |
| Pod | Kubernetes execution unit containing the job containers and their assigned volumes |
| Namespace | Kubernetes grouping/access scope for resources; not a VM or complete security boundary alone |
| RBAC | Which Kubernetes API operations a service account may perform |
| UID | Kubernetes object identity; distinguishes a replacement object from an earlier object with the same name |
| Ledger | The coordinator's durable record of execution identity, lifecycle and observations |
| CRI | Container Runtime Interface; exposes runtime identity/state and standardized log records |
| Digest/hash | A deterministic fingerprint of bytes; this project uses SHA-256 for evidence integrity |
| Canonical JSON | A specified JSON representation so equivalent formatting choices do not create ambiguous signed inputs |
| Signature | Cryptographic binding of bytes to a key; verified using public material |
| Certificate | A signed binding between a public key and identity |
| Trust root | Public CA/log/TSA material explicitly trusted by a verifier |
| OIDC token | An issuer-signed identity statement used to obtain a short-lived signing certificate |
| mTLS | TLS where the client also presents a certificate; used to authenticate the private finalizer to its issuer |
| JIT registration | GitHub's just-in-time configuration for the assigned single-use runner |
| STS session | Temporary AWS credentials obtained by assuming a role |
| Idempotency | Repeating an operation without inventing a second logical result for the same execution |
| Fail closed | Missing required evidence is not silently interpreted as success |

## Component by component

### GitHub: scheduling and application results

The committed workflow identifies the required runner using
`proof-gh-RUN-ATTEMPT`. The coordinator admits only the configured repository,
branch, workflow, job and exact source SHA. Pull requests, forks and arbitrary
new branch commits are not automatically admitted by this demo controller.

GitHub records step outcomes. The coordinator later verifies GitHub's mapping
of run, attempt, source, job and registered runner, plus job completion. The
runner registry is separately checked for absence after disposal.

The GitHub listener may exit successfully even when an application step fails:
it successfully executed/reporting the job. Therefore a Kubernetes exit code is
not a substitute for the GitHub job/step conclusion.

The separate `cleanup/signed-receipt` commit status is posted after receipt
acceptance. It is a commit-level status, so subsequent runs of the same SHA can
change its latest value. Historical truth is keyed by run ID and attempt.

### Laptop bridge, cloud broker and SSM

The local bridge uses the laptop's existing GitHub login directly with GitHub.
It permits a narrow set of repository-specific API paths and validates payloads
and the source pin. It does not export the administrative token to EC2.

The cloud broker is a loopback HTTP request queue. SSM port forwarding connects
the laptop to that queue over encrypted transport. The bridge returns permitted
API responses, including one-use runner registration, to the waiting cloud caller.
Another tunnel provides private access to the dashboard.

This is an operator-assisted deployment. Losing the laptop bridge affects new
job admission and GitHub verification/status publication. It is not a fully
unattended GitHub App integration. A dedicated repository-scoped GitHub App is
a future replacement, not an implemented capability you should claim today.

### EC2, Docker and kind

EC2 supplies the real AWS compute host. Its encrypted EBS volume holds prepared
software, Docker state, metadata and private signing infrastructure. Docker runs
the services. kind supplies Kubernetes by running its node inside a Docker
container. The CI pod is scheduled inside that node.

This arrangement preserves the node access needed by the existing observer.
It is ordinary EC2/container isolation, not EKS, Fargate or Firecracker. Docker
inside this architecture does not mean each runner has its own virtual machine.

### Coordinator

The coordinator is the trusted lifecycle owner. It checks admission, registers
the GitHub runner, records identities/UIDs, provisions scoped resources, arms
observation, launches the job, collects evidence and verifies disposal.

Its durable phases include provisioning, launching, running, collecting,
disposing and complete. A singleton lock prevents competing local controllers.
It records intent and identities so recovery does not blindly replay job commands.

Names are insufficient for safe deletion. If a namespace is deleted and another
appears with the same name, its UID changes. UID checks/preconditions help avoid
deleting or treating that replacement as the original object.

### Runner and native guard

The runner runs as a non-root user with scoped Kubernetes access. It receives
one-use GitHub registration material; it does not receive archive/signing roles
or the operator's GitHub administration token.

The guard main creates the assigned workspace and credential directory. The
application uses those locations. GitHub invokes the guard's native post step
to attempt cleanup after the application, including a failed application step.

The filesystem cleaner pins directory identities and uses descriptor-relative
operations. It refuses unsafe traversal, symlink/mount escapes and changed
directory identities rather than escalating privileges to make cleanup pass.
It unlinks symlinks inside the allowed directory rather than following them.

The launcher separately removes its disposable GitHub runtime and exits. These
are cleanup attempts by the workload side; external observation is still required.

### Network gate and proxy

The gate prevents execution from outrunning setup of the restricted networking
and the pre-start observer. Kubernetes policy and the per-pod gate restrict the
runner's reachable destinations. Its GitHub HTTPS traffic uses a CONNECT proxy
that checks allowed names and resolves only public IP addresses.

This is destination restriction, not HTTPS-content inspection or a general
data-loss-prevention guarantee. Allowed GitHub-related domains have broad uses;
do not claim the design prevents every possible exfiltration channel.

The host requires IMDSv2 and blocks container traffic to the metadata endpoint.
This prevents the demonstrated container path from retrieving the EC2 role's
credentials. It is not proof that a kernel escape could never reach them.

### External observer

The observer is a trusted process in the kind node, outside the job container.
It is armed before the runner begins and binds its request to exact runtime
and Kubernetes identities. It follows bounded raw CRI stdout/stderr bytes,
records timing and checks for gaps, replacement, rotation and truncation.

After runtime termination, before pod disposal, it inspects the pinned assigned
volume and scoped workspace/credential directories, including hidden entries.
The GitHub runtime cleanup is also part of this profile's stopped-runner check.

It reports `verified`, `failed` or `unobservable` findings. A missing observation
window cannot be recreated after the fact and called continuous coverage.
The raw log cap is 100 MiB per job; exceeding bounds does not produce a full pass.

The observer shares the trusted node/kernel. “External” means outside the job's
execution boundary, not outside AWS or on a different physical host.

### Evidence files and readiness marker

`run.json` records the ledger and bindings. `collector.json` records the trusted
observer output. `job.log` contains exact bounded CRI bytes. `cleanup-evidence.json`
and `guard.json` are preliminary job-side reports. `observations.json` derives
coverage using the trusted sources and binds artifact hashes.

`finalization-ready.json` is written last. It binds the ledger and observations
hashes, making an incomplete multi-file handoff distinguishable from a committed
snapshot. Merely seeing `phase: complete` is insufficient to start signing.

These files are not self-authenticating simply because they contain hashes.
The finalizer accepts them from its operator-controlled read-only input mount.

### Finalizer

The finalizer validates identities, readiness, schema, artifact hashes, coverage
and policy. It freezes the first complete input snapshot for that execution,
detects conflicting retries and uses durable state for publication recovery.

Its separate pinned Docker image has a read-only root filesystem, non-root UID,
dropped capabilities, resource limits, restricted mounts and only the required
signing/archive networks. It cannot write the coordinator input mount or directly
write PostgreSQL metadata. It has writable private retry/output volumes; saying
the entire container has no writable storage would be incorrect.

The finalizer builds and signs the structured receipt. It preserves negative
job findings conservatively; it does not replace them with a more favorable
observation. In the blocked case, the final receipt's workspace finding is marked
`observer: job`, while archived observations separately prove the external
collector also found residue.

### Private issuer, Fulcio, CTLog, Rekor, TSA and Cosign

1. The finalizer authenticates to the private issuer with its mTLS client identity.
2. The issuer returns a signed OIDC identity token for the finalizer.
3. Cosign creates an ephemeral artifact-signing key pair. Fulcio validates the
   identity and issues a short-lived certificate binding the public key to it.
4. CTLog supplies certificate-transparency evidence, including an SCT checked
   against the configured CT trust. An SCT is a signed promise; do not describe
   it as a separately demonstrated CT inclusion proof unless you verify one.
5. Cosign signs the receipt bytes. Rekor supplies signature-log inclusion evidence
   and a signed checkpoint. The bundle also includes RFC3161 timestamp evidence.
6. Verification uses the explicitly configured public trust root, signer identity
   and issuer. Exported bundles can be checked offline.

“Keyless” means no long-lived artifact-signing key managed by the job/finalizer
for routine signing. It does not mean there are no keys: infrastructure CA/log/TSA
keys and the finalizer's mTLS authentication key persist in protected volumes.

The signer identity is `finalizer@cleanup-receipt.local`, and the issuer is
`https://issuer:8443`. This is your private identity system, not GitHub public OIDC
and not the public Sigstore trust ecosystem. The private trust root must be
obtained through a trusted channel, not accepted from an arbitrary submitted receipt.

The TSA currently trusts the host clock with public NTP monitoring disabled.
It provides signed timestamp evidence under that assumption, not an independent
external guarantee of correct wall-clock time. Private transparency logs are
also operated within the same trust domain; no independent witness federation
or broad public monitoring has been demonstrated here.

### S3, versioning, Object Lock and KMS

S3 stores the actual evidence objects, receipt and signature bundle. References
carry bucket, content-addressed key, exact version ID, SHA-256 and size. The hash
binds the bytes; the version ID removes ambiguity about which stored version
was verified. Versioning alone does not prevent a privileged deletion.

Compliance Object Lock prevents early deletion/shortening of protected versions.
The bucket defaults to eight days so the verifier's minimum seven-full-day
retention requirement has a rounding margin. Actual per-version deadlines are
authoritative. Lifecycle expiration is asynchronous and does not override a lock.

SSE-KMS encrypts evidence at rest using the dedicated KMS key. KMS is not the
Sigstore signing key. Encryption is confidentiality, signatures are integrity/
identity, Object Lock is retention protection: those are different properties.

The host assumes distinct STS writer and reader roles. Sessions last one hour
and are refreshed every fifteen minutes into separate private volumes. The
archive client rereads its explicit session file rather than falling back to
ambient environment/profile/metadata credentials.

Writer: scoped put/get/retention-extension and relevant KMS permissions, no
deletion. Reader: scoped reads and KMS decryption, no writes/deletion. The host
can assume both roles, so this separation does not isolate against a hostile host.

Object Lock does not ensure availability if the encryption key is disabled or
lost. Keep the KMS key and exported trust/evidence available through retention.
The old MinIO Go S3 client library remains; no MinIO or KES server runs in AWS.

### API, delivery queue, PostgreSQL and dashboard

The finalizer's publication is delivered to the API. The API downloads exact
referenced versions, checks hashes/sizes, parses strict schemas, enforces expected
identity/policy, verifies the signature, and validates archived evidence before
writing accepted metadata. It can reject a valid signature with the wrong policy.

The delivery queue tracks attempts and preserves failures. Retrying delivery is
not rerunning the CI job. Conflicting payloads for the same logical identity are
not silently overwritten. Bounded retry/recovery is implemented; indefinite
availability and recovery from all possible infrastructure losses are not claimed.

PostgreSQL stores searchable metadata, verdicts, references, incidents and retry
information. The dashboard shows that data. Detailed reasons are sanitized in
metadata; use the signed archive for exact observations. The database row alone
is not the proof, and a dashboard screenshot is not cryptographic verification.

The API's verification endpoint can recheck an existing receipt against its current
policy and dependencies. Policy upgrades can affect a historical online recheck;
offline verification against the pinned historical trust is a different operation.

### CloudFormation and operational services

CloudFormation declares the VPC, subnet, routing, S3 gateway endpoint, buckets,
KMS key, IAM roles, encrypted host volume and EC2 instance. No inbound security
group ports are open. SSM supports management via outbound connections. The S3
gateway endpoint avoids a billed NAT gateway for S3 access.

Systemd resumes the prepared stack, keeps the controller/broker/proxy running,
refreshes archive credentials and installs the metadata firewall. The per-boot
eight-hour stop timer limits a forgotten running host; it is not a spending cap.
The deploy-assets bucket stores committed source delivery artifacts, not the
evidence archive. Private signing/metadata state persists through host restart.

## Exactly what the five checks mean

| Check | Passing claim | Not claimed |
| --- | --- | --- |
| Workspace | Assigned stopped-runner filesystem scope was absent/empty as defined by the observer | Physical block erasure, all `/tmp` files, every external copy or backup |
| Credentials | Scoped credential files empty/absent plus run-scoped RBAC and runner disposal | Revocation of real external tokens or account-wide secrets |
| Resources | Assigned namespace absent, bound to original cluster/namespace identity | Deletion of the EC2 host, evidence buckets, arbitrary AWS resources or unsupported persistent volumes |
| Logs | Bounded watched CRI stdout/stderr complete under the observer's continuity checks | Every possible log/file/network event, or deletion of logs—the evidence logs are retained |
| Runner disposal | Matching GitHub assignment/completion, registration absent, and UID-bound Kubernetes/RBAC disposal | Destruction of a separate VM or proof of physical memory zeroing |

Verdict rule: any failed required check means `fail`; otherwise any required
unobservable check means `partial`; all required checks verified by trusted observers means `pass`.
Signature state is independent: pass, partial and fail receipts can all carry
valid signatures.

## The evidence chain to trace in front of judges

Choose one fresh run. Correlate:

1. GitHub run ID, attempt, job and approved source commit.
2. Coordinator identity and cluster/namespace/pod UIDs.
3. Observer request binding, armed/start/finish times and residue/log findings.
4. Ledger and observation hashes in the readiness marker.
5. Signed receipt identity, verdict and exact evidence references.
6. S3 object version, SHA-256, KMS encryption and retention deadline.
7. Offline signature verification using independently pinned public trust.

The signature covers the receipt bytes. The receipt carries hashes/versioned
references to observations. To authenticate an exported observation, verify the
receipt signature and compare the observation bytes' hash with that signed
reference. Do not modify JSON formatting and expect the original byte signature
to remain valid. Public readability of a bundle does not make its trust root trusted.

## Demonstration order for a knowledgeable panel

Suggested rehearsal: 15–20 minutes, with extra time for questions and setup.
This is a presentation estimate, not a measured performance claim.

| Stage | Show | Explain |
| --- | --- | --- |
| Opening | Architecture and exact source pin | The bounded proof and trusted components |
| Normal run | Fresh GitHub job, pod/ledger watches, synthetic files during pause | Real execution and independent observation timing |
| Receipt | Five checks, source/run identity, archived observations | Which component supports each claim |
| Cryptography | Exact S3 version and offline verification, then altered-byte rejection | Storage metadata versus byte integrity versus identity |
| Failed application | Wrong arithmetic assertion, successful guard post, cleanup pass | Application result differs from cleanup result |
| Blocked cleanup | Correct calculation, failing post, external residue, signed fail | Failure is measured and preserved, not hidden by pod deletion |
| Recovery | New normal run, no remaining registration/namespaces | Isolation between attempts and completed disposal |
| Finish | Retention and stop command | Operational ownership and remaining costs |

Run watches before dispatch. Keep the laptop bridge/tunnels open. Do not open
secret files to “show everything”; show roles, mount restrictions and metadata
instead. The purpose is explainable evidence, not an unreadable stream of output.

## Capabilities: demonstrated versus future work

Demonstrated in AWS: real GitHub jobs, scoped synthetic workload, normal/negative
cleanup cases, S3/KMS archive, verified signatures and tamper rejection, IAM denied
operations, metadata network blocking, actual EC2 restart/resume, metadata backup
restore into a temporary database, and absence of completed demo registrations.

Implemented but not exhaustively live fault-tested across AWS: all retry/crash
windows, all network attacks, every filesystem edge case and prolonged outages.
Some additional failure/isolation tests belong to the earlier local release;
do not present those measurements as fresh AWS experiments.

Future work: unattended GitHub App operation, broader workload/network support,
multi-tenant scheduling and scaling, separate trust domains, automated certificate
renewal/rotation, independent transparency witnesses/time assurance, production
capacity testing and external security review. None is implied by a green demo.

## Questions to practice answering without reading

1. Why isn't GitHub success enough? Because application success is not independent cleanup evidence.
2. Why not sign inside the job? It would combine the subject of the claim with signing authority.
3. Why observe before pod deletion? To preserve evidence of residual files instead of hiding them behind disposal.
4. Why UIDs? Names can be reused; deletion and observation must refer to the original objects.
5. Why private Sigstore? Controlled trust/integration, with the explicit cost of operating and trusting the private infrastructure.
6. Why both Rekor and S3? Log inclusion evidence and retained retrievable artifact bytes are different functions.
7. Why both hashes and signatures? Hashes identify bytes; signatures bind them to a trusted signing identity.
8. Why can a signed failure be valid? Validity authenticates the report, not a positive outcome.
9. Why can a failed GitHub job have a pass receipt? Its application assertion failed while cleanup checks succeeded.
10. Why is finalizer Docker isolation insufficient against root? Host root controls the kernel, mounts and secrets.
11. What happens if observation is missing? Required coverage is incomplete or finalization cannot proceed; no invented pass.
12. What happens if the bridge disappears? Admission/GitHub verification can fail or stall; unattended operation is not implemented.
13. Does keyless mean secretless? No: ephemeral artifact key, persistent protected infrastructure/authentication keys.
14. Does encrypted mean undeletable? No; encryption and retention lock solve different problems.
15. Can locked evidence become unreadable? Yes, losing/disabling its KMS key threatens availability.
16. Is this Firecracker? No. It is EC2 plus Docker/kind using temporary Free-plan credits.
17. Are logs deleted? The scoped logs are intentionally retained as evidence; the log check concerns completeness.
18. Are external credentials revoked? No; the claim covers assigned credential files and scoped RBAC.
19. Is it production-ready? It is a validated bounded prototype, not a multi-tenant hardened service with an SLA.
20. What demonstrates implementation? Fresh correlated executions, negative cases, stored evidence and independent verification—not a claim about how code was authored.

## Code locations to study

Paths are relative to [the isolated AWS checkout](ephemeral-runner-cleanup-receipt-aws/README.md).

| Question | Read |
| --- | --- |
| What executes on GitHub? | `.github/workflows/cleanup-aws.yml`, `examples/aws-demo/order-export.mjs` |
| What chooses/registers/verifies the runner? | `internal/githubci/client.go`, `cmd/githubci/main.go` |
| What does the runner launch and remove? | `infra/github/entrypoint.mjs` |
| What runs before/after the application? | `action/src/main.ts`, `action/src/post.ts`, `action/src/lifecycle.ts` |
| How are deletes constrained? | `internal/proof/files_linux.go`, `internal/proof/kubernetes.go` |
| What observations are trusted? | `internal/coordinator/collector.go`, `internal/coordinator/observer/`, `internal/coordinator/observations.go` |
| How are verdicts and retries formed? | `internal/finalizer/core.go`, `internal/finalizer/schema.go` |
| How is finalizer isolation configured? | `compose.finalizer.yaml` |
| How does signing work? | `infra/sigstore/README.md`, `internal/finalizer/signing.go` |
| What does independent API verification do? | `internal/api/verify.go`, `internal/finalizer/archived.go` |
| How are AWS identities separated? | `infra/aws/template.py`, `scripts/aws/sessions.py`, `internal/archive/session.go` |
| How does the laptop bridge restrict requests? | `scripts/aws/github-bridge.py`, `infra/github/broker.py` |
| What is allowed on the network? | `infra/github/proxy.py`, `cmd/netgate/`, Kubernetes policy scripts |
| How do I resume, watch and tear down? | `scripts/aws/resume.sh`, `scripts/aws/watch-demo.py`, `scripts/aws/teardown.py` |

Read the code for a claim before saying it. If asked about an untested property,
say what is implemented, what has been measured and what still needs validation.
