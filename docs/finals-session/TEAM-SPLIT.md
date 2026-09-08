# Three-person responsibility split

This is suggested ownership, not a statement of historical authorship.

| Role | Stack | Implementation responsibility | Live demonstration |
| --- | --- | --- | --- |
| CI and cloud infrastructure | GitHub Actions/YAML, Go coordinator/client-go, Docker/kind, EC2, CloudFormation, Python/Bash, IAM/SSM | Provision infrastructure; admit pinned source; request JIT runner configuration through the laptop GitHub bridge; start a one-job pod with a unique run/attempt label; track Kubernetes and GitHub runner disposal. | Show the workflow, source commit, live runner pod and subsequent disposal. |
| Cleanup and evidence | TypeScript/Node main and post Action hooks, Go cleanup/coordinator/finalizer, Linux filesystem and CRI log observation, JSON/SHA-256 | Prepare run-scoped directories; attempt post-job cleanup; independently inspect the stopped runner; combine observer and coordinator findings; create a pass/fail/partial receipt in a restricted finalizer container. | Explain clean success, application failure with clean disposal, and deliberately blocked cleanup with observed residue. |
| Signing, storage and verification | Private Fulcio/Rekor/CTLog/TSA and Cosign; S3/KMS; PostgreSQL/pgx/SQL migrations; Go/Chi API; React/TypeScript/Vite/Tailwind/TanStack; Playwright | Integrate receipt signing; archive versioned evidence with retention; verify signatures and references before API acceptance; index metadata and incidents; display coverage and fresh verification. | Show original evidence references, signature checks, a valid failure receipt and rejection of an altered local copy. |

## Component meanings

The coordinator supervises the run. The guard attempts cleanup. The external
observer checks outside the job, but remains inside the trusted host environment.
The finalizer evaluates evidence and signs through the private Sigstore services.
Its non-root, read-only Docker container drops capabilities and restricts mounts;
it still shares the host kernel.

Fulcio binds the finalizer identity to an ephemeral signing public key. CTLog
supports certificate transparency; Rekor supports signing transparency; TSA
supplies signed timestamp evidence. Cosign signs and verifies. Persistent private
infrastructure keys still exist: keyless does not mean no keys.

S3 holds evidence bytes; KMS protects encryption, not receipt signing. Version IDs
select exact objects and SHA-256 binds exact bytes. Object Lock protects retention
for a finite period. PostgreSQL indexes metadata and incidents. The API verifies
evidence; the dashboard presents the API results.

## Shared handoff and limits

Role 1 provides exact run, attempt, source and runner identities. Role 2 binds
observations and a verdict to those identities. Role 3 preserves and verifies the
signed references. Roles 2 and 3 jointly own the receipt/signing contract.

The demo is a synthetic order-export workload executing on real AWS/GitHub
infrastructure. It uses Docker/kind on EC2, not Firecracker. The GitHub bridge
depends on the operator laptop. Host, observer and private signing infrastructure
remain trusted. This is scoped cleanup evidence, not physical erasure proof.
An application failure can have clean disposal; a signature can validly attest
to a failure receipt. Each presenter should explain their input, output, failure
case and trust limitation, and accurately acknowledge assisted implementation.
