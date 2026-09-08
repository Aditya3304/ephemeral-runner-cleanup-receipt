# AWS finals deployment

This profile extends committed local release `4bc28ba15cb6ec85fec947ef73f231611c78b63c`.
It runs the existing coordinator, node observer, isolated finalizer, private
Fulcio/Rekor/CTLog/TSA, PostgreSQL and dashboard on a dedicated EC2 host. A real
one-job GitHub Actions runner executes inside its `kind` cluster. Amazon S3 with
SSE-KMS, versioning and seven-day Compliance Object Lock replaces the MinIO/KES
services. The S3-compatible Go client library is retained; no MinIO server runs
in this profile.

## Cost and Firecracker decision

Account checked through AWS on 8 September 2026: active **Free** plan,
**$94.74** credits, expiration **7 January 2027**. AWS explicitly reports
`m7i-flex.large` as Free Tier eligible for this account model. This is temporary
credit-funded use, not a recurring free allowance for an always-on stack.

| Path | Honest assessment |
| --- | --- |
| Self-managed Firecracker | Requires usable KVM. The claim that AWS always requires metal is now outdated: current EC2 documentation supports nested virtualization on selected instance families, including m7i-flex. KVM must be enabled and tested before claiming this works. This deployment does neither. Metal is unsuitable for this budget: Linux us-east-1 list examples are m5.metal $4.608/hour and i3.metal $4.992/hour. |
| Fargate | AWS operates Firecracker-backed isolation; the customer does not operate the VMM. There is no recurring free Fargate compute allowance. Promotional credits can fund it. It removes the host/CRI/filesystem observation access that this project's existing proof depends on, so substituting it requires a different observer and narrower claims. |
| Lambda functions | Firecracker-backed execution, 1 million requests and 400,000 GB-seconds/month in the standard free allowance. Fifteen-minute maximum invocation; execution environments and `/tmp` may be reused. An invocation is not proof of a newly created or destroyed microVM. Useful for a separate verification function, not a drop-in Kubernetes CI runner. |
| Micro EC2 + k3s/kind | t2.micro/t3.micro have only 1 GiB. They do not realistically fit this complete stack and its builds. Ordinary EC2 deployment is not a Firecracker deployment. Historical 750-hour micro allowances must not be applied to this newer credit-based account. |
| Other microVM path | A small Lambda-based verifier could support an honest hybrid Firecracker-backed-service claim while runners stay elsewhere. New Lambda MicroVMs lifecycle APIs are another research path, but their separate pricing must not be confused with the functions allowance. Neither proves physical memory or disk erasure. |

**Chosen option:** one `m7i-flex.large` (2 vCPU, 8 GiB), `kind`, existing separated
services, native GitHub Actions runner, and real S3. It preserves the existing
evidence model and minimizes changes. **There is no Firecracker claim for this
deployment.** EC2/Nitro virtualization and ordinary Kubernetes containers are
the actual boundaries.

Primary references: [EC2 Free Tier eligibility](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-free-tier-usage.html),
[nested virtualization](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/amazon-ec2-nested-virtualization.html),
[Free Tier plan FAQ](https://aws.amazon.com/free/free-tier-faqs/),
[Fargate pricing](https://aws.amazon.com/fargate/pricing/),
[Lambda pricing](https://aws.amazon.com/lambda/pricing/),
[EC2 pricing](https://aws.amazon.com/ec2/pricing/on-demand/).

## Resources and order

`python3 infra/aws/template.py` emits the CloudFormation template. Use us-east-1;
the archive endpoint and deployment scripts deliberately pin that region.

1. Check account plan/credits and save the stack resource outputs.
2. Create the dedicated VPC, one subnet, internet gateway and routes. The free
   S3 gateway endpoint avoids a NAT gateway. No inbound security-group rules.
3. Create the evidence KMS key, private versioned Object Lock bucket, private
   short-lived deployment-assets bucket, and separate host/writer/reader roles.
4. Create encrypted 40 GiB gp3 storage and the EC2 instance, requiring IMDSv2
   with hop limit one. Host firewall also rejects container access to metadata.
5. Prepare pinned Go, kubectl, kind, Node, Sigstore and runner build inputs.
   Deploy a Git bundle containing only committed source. Never copy the dirty
   original laptop checkout.
6. Run `sudo -H bash scripts/aws/up.sh` on the dedicated host. It builds and
   starts PostgreSQL, kind/network policy, runner image, private signing
   services, AWS archive sessions, finalizer and dashboard.
7. Start SSM port forwarding from laptop local port 18123 to host loopback 8123,
   then run `python3 scripts/aws/github-bridge.py --revision EXACT_COMMIT` on the
   laptop. The bridge uses the existing local `gh` login directly with GitHub.
   No administrative GitHub token is exported to AWS.
8. Forward laptop port 18080 to host port 8080 for the dashboard. Keep both SSM
   sessions and the GitHub bridge running during the demo.
9. Trigger the committed `.github/workflows/cleanup-aws.yml` on the approved
   feature branch. The controller pins the exact source SHA; it does not admit
   arbitrary future pushes, pull requests or forked source.

This demo has an operator laptop dependency for the restricted GitHub API bridge.
The runner, observer, signing services, evidence and metadata run in AWS. If the
bridge becomes unavailable, the system cannot claim verified GitHub disposal.
The cloud queue stores API requests/JIT responses only in memory; SSM encrypts
their transport. A dedicated repository-scoped GitHub App could replace this
operator bridge in a later unattended deployment.

## GitHub lifecycle and permissions

The controller polls only the approved workflow, creates GitHub's JIT runner
configuration, and assigns a unique label `proof-gh-RUN-ATTEMPT`. The registered
runner executes one job. Its native action main/post creates and cleans the
assigned workspace. No Docker socket or AWS signing/archive role enters the job.
GitHub traffic goes through a private HTTPS CONNECT allowlist; direct metadata,
S3, arbitrary internet and host-service access is blocked by the runner gate.

The external observer starts before the job, collects bounded CRI stdout/stderr,
and checks the pinned stopped-runner volume, including the Actions runtime.
The coordinator verifies GitHub run/job/runner/source assignment, registration
absence, namespace UID and RBAC disposal. The isolated finalizer signs the
result under the existing private Sigstore identity. The receipt API independently
verifies the signature, policy, archived object versions and metadata before
acceptance. Only then does GitHub receive `cleanup/signed-receipt` success/failure.
The ordinary workflow conclusion and cleanup verdict are different observations.

| Principal | Permissions |
| --- | --- |
| Provisioning operator | CloudFormation and creation/deletion of the listed EC2, VPC, S3, KMS and IAM resources. Temporary AWS CLI login was used for bootstrap; no root access key was created. |
| EC2 host role | AWS managed SSM instance permissions, read deployment assets, assume only the evidence writer/reader roles. |
| Finalizer writer session | Put/get evidence objects and versions, read/extend retention only under `evidence/*`; KMS GenerateDataKey/Decrypt restricted through regional S3 and the evidence object encryption context. No deletion or bucket administration. |
| API reader session | Get evidence objects/versions and retention, KMS Decrypt with the same S3/context restriction. No writes or deletion. |
| GitHub workload | `contents: read`; no `id-token: write` and no AWS role. |
| Operator GitHub API | Repository Administration write for JIT runner registration/deletion, Actions read, commit statuses write. The local bridge additionally restricts paths, payloads, repository and status SHA. |

Writer/reader STS sessions rotate every fifteen minutes and expire in one hour.
Only the trusted host writes them into separate private volumes. The archive
client has no environment/IMDS fallback. KMS protects evidence encryption; it is
not the Sigstore signing key. The private Fulcio identity belongs to the finalizer,
not GitHub's public OIDC service.

## Estimated exposure

US East (N. Virginia), Linux rates checked 8 September 2026, excluding tax:

| Resource | Approximate list exposure |
| --- | --- |
| m7i-flex.large | $0.09576/hour |
| One public IPv4 while running | $0.005/hour |
| 40 GiB gp3 | $3.20/month while the volume exists, including stopped time |
| One customer-managed KMS key | $1/month, prorated; requests beyond eligible allowance extra |
| S3 Standard | $0.023/GB-month; $0.005/1,000 PUTs; $0.0004/1,000 GETs |
| S3 gateway endpoint | No hourly charge |

Twenty hours of compute plus seven days of 40 GiB storage is about **$2.77**,
plus roughly $0.23 for the retained KMS key, small S3/request charges and any
chargeable transfer. A full 30 days of compute/IPv4/storage is approximately
**$75.75**, plus KMS/S3. The eight-hour stop timer limits one host boot session;
it is not an account-wide hard spending cap. Free-plan credits are temporary.
Do not upgrade the account or add NAT gateways, EKS, load balancers, RDS or
large instances for this demo without separately assessing their costs.

See [S3 pricing](https://aws.amazon.com/s3/pricing/),
[KMS pricing](https://aws.amazon.com/kms/pricing/) and
[VPC pricing](https://aws.amazon.com/vpc/pricing/).

## Stop, resume and teardown

1. Stop admitting jobs; let the active run finish collection/finalization.
   Stop `cleanup-github.service` before maintenance. Preserve incomplete ledgers.
2. Export signed receipts, bundles, public trust, validation records and a metadata
   backup if you need them after termination. Verify exported files before deletion.
3. End the two SSM tunnels and local GitHub bridge. Inspect repository runners
   and remove only this demo's inactive `proof-gh-*` registrations if any remain.
4. For a pause, stop the EC2 instance. The automatic public IPv4 is released;
   EBS, S3 and KMS remain chargeable. On resume, start the prepared stack before
   admitting new jobs and reconnect the laptop bridge.
5. For permanent teardown, empty only the deployment-assets bucket, then delete
   the `cleanup-finals` CloudFormation stack. Its EC2 root volume is configured
   to delete on termination. Confirm instance, volume, IAM roles, network and
   endpoint removal. The evidence bucket and KMS key are intentionally retained.
6. **Compliance Object Lock cannot be bypassed, including by root.** After the
   latest retention deadline, delete retained object versions and delete markers,
   then delete the evidence bucket. Lifecycle rules assist expiration but are
   asynchronous and are not proof of completed deletion.
7. Keep the KMS key usable until evidence is exported or expired. Then schedule
   key deletion with the minimum permitted seven-day waiting period. Record
   retained resources and check Billing again after usage reporting catches up.

Seven-day retention can be extended when a receipt is admitted again, so use the
actual per-version retention dates, not simply seven days after the first demo.
[Object Lock documentation](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lock.html).

## Exact judge talking points

“GitHub Actions schedules a genuine single-job self-hosted runner in a Kubernetes
cluster on AWS EC2. We moved the existing trusted coordinator, external observer,
isolated finalizer and private Sigstore services into AWS without changing their
roles. Evidence is now stored in private, versioned Amazon S3 with KMS encryption
and Compliance Object Lock; PostgreSQL holds receipt metadata.”

“The job attempts cleanup, but its own report is not sufficient. An observer
outside the job inspects the stopped runner's assigned filesystem and runtime
logs. The coordinator checks Kubernetes UIDs, RBAC disposal and the GitHub runner
registration. A separate finalizer signs those bounded observations, and another
service verifies the receipt before accepting it.”

“This deployment does not run Firecracker. We selected credit-funded EC2 because
it preserves the host-level observations our current proof requires. Lambda and
Fargate use AWS-managed isolation, but adopting them would require changing what
we can observe and claim. We do not equate a completed managed invocation with
proof that a fresh microVM was created or that physical memory was erased.”

“Our receipt proves the defined observations under a trusted operator, Kubernetes
node and private signing infrastructure. It does not prove physical block erasure,
defend against a malicious host administrator, or claim AWS destroyed a machine.
All components share one EC2 host for the demo; separation is by processes,
containers, identities, mounts and network policy, not independent physical hosts.”

“The GitHub administration credential stays on my laptop. A restricted bridge over
Systems Manager obtains single-use registration and verifies GitHub metadata.
This is a temporary demo deployment funded by Free-plan credits, not an assertion
that the full architecture is permanently free.”
