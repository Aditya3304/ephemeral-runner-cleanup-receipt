# What happens from beginning to end

The project produces signed, verifiable evidence of defined cleanup observations
for a specific CI execution. Cryptography protects the recorded evidence; the
observer and coordinator supply the facts being recorded.

## Opening pitch

CI systems tell us whether a job finished and whether its tests passed. For an
organization operating its own runners, another question remains: what evidence
shows that its assigned temporary environment was cleaned afterward?

Our cleanup-attestation layer gives each admitted GitHub execution a temporary
Kubernetes runner. A guard attempts cleanup, an observer outside the job checks
the stopped runner, and a coordinator checks associated resources and runner
registration. A separate finalizer signs a receipt referencing archived evidence.

The demonstration uses synthetic application data, but real GitHub jobs, AWS
compute, filesystem operations, signing and S3 storage. It trusts the host and
private observation/signing infrastructure. It does not establish physical
erasure, universal data absence or protection from a malicious host administrator.

## The one-command launcher

In the original prepared laptop session, `bash outputs/Run-All-Scenarios.sh`
starts Python orchestration and uses `tee` to display and save its output.
`demo-console.py` runs normal, test-failure, cleanup-blocked and normal sequentially.
`run-walkthrough.py` dispatches GitHub workflows and captures progress.
`evidence_view.py` downloads and verifies original evidence. `read_retry.py`
retries selected transient read failures, not non-idempotent job dispatches.

This is not an infrastructure installation command. The host services, local
authentication, bridge, tunnels and pinned tools must already be available.

## Execution sequence

1. The laptop uses its existing `gh` login to dispatch `cleanup-aws.yml` with a
   scenario input. GitHub assigns a run ID, attempt number and source revision.
2. The workflow asks for label `proof-gh-RUN-ATTEMPT`. The Go coordinator admits
   only configured source/workflow identities and supplies the matching runner.
3. A restricted Python bridge on the laptop performs permitted GitHub operations
   through an SSM-connected cloud broker. The administrative token stays local.
   A separate SSM tunnel exposes the AWS dashboard at localhost:18080.
4. The coordinator provisions a scoped Kubernetes runner pod in kind on EC2,
   records resource UIDs and establishes observation/network readiness.
5. The TypeScript guard calls Go `proofctl begin` and `inventory`, preparing
   directories and recording owned resource identities. State lives outside the
   disposable workspace. The application runs inside that assigned scope.
6. The synthetic order export calculates 4999 cents, writes input/output files
   and a dummy credential, and pauses for inspection. The test-failure case
   deliberately expects 5000. The blocked case creates a non-writable child
   directory that the configured non-root cleaner cannot completely empty.
7. Native Action post calls `proofctl cleanup` and then `receipt`. The latter is
   unsigned preliminary job-side evidence, not the final signed receipt.
8. The observer, armed before execution, inspects the stopped runner before pod
   disposal. It collects bounded CRI logs and checks scoped directory contents.
   The coordinator checks identity-bound resources and GitHub runner disposal.
9. The durable ledger and finalization-ready marker bind an assembled input
   snapshot. The restricted finalizer evaluates readiness, identities, hashes,
   schema and coverage, retaining failures and detecting conflicting inputs.
10. The finalizer signs through private Sigstore. S3 stores exact evidence
    versions with hashes, encryption and retention. The API verifies before
    acceptance; PostgreSQL indexes metadata and incidents; React displays them.
11. The laptop helper fetches exact versions, checks hashes, verifies offline
    signatures, rejects an altered local copy and requests fresh API verification.

Names can be reused; Kubernetes UIDs distinguish replacement resources. Observing
before disposal preserves a residue finding rather than hiding it behind later
destruction of the environment. Missing observation cannot be recreated and
called continuous coverage. Some outages leave processing pending rather than
immediately producing a receipt.

## Technology roles

EC2 hosts Docker services and the kind Kubernetes node. The observer is outside
the job but within the trusted node environment. The finalizer is a non-root,
restricted Docker process, with a read-only root and selected writable volumes;
it shares the host kernel. Neither component has independent hardware trust.

Private issuer authentication supplies the finalizer identity. Fulcio binds an
ephemeral public key to it. CTLog supports certificate transparency, Rekor signing
transparency, and TSA signed timestamp evidence. Cosign signs and verifies.
Keyless does not mean secretless: private infrastructure/authentication keys
persist. An SCT is a signed promise, not by itself a demonstrated inclusion proof.

SHA-256 identifies exact bytes. A signature authenticates the signed record under
configured trust. S3 version IDs select precise objects. Object Lock protects
versions during finite retention. KMS protects storage encryption, not receipt
signing. Losing key access threatens evidence availability despite retention.

PostgreSQL is the searchable catalogue, not the proof. The Go/Chi API verifies
evidence and serves the React/TypeScript dashboard. Coverage counts are not
confidence percentages. Logs coverage means bounded completeness, not deletion.
Credential coverage does not establish revocation of arbitrary external tokens.

## Live presentation order

Show the pinned workflow, dispatch a fresh run, correlate run/pod identities,
inspect synthetic files during the pause, and follow the resulting receipt.
Show failed application plus clean cleanup, then obstructed cleanup plus external
residue. Verify the signed evidence and reject an altered local copy. Finish with
a new normal run while preserving the prior incident. This last run is not a
host restart test. Label any earlier receipt as historical if used as a fallback.

## Contribution and limits

The contribution is the integrated evidence protocol: separate cleanup and
observation, inspect before disposal, bind to exact executions, preserve negative
or incomplete outcomes, and make retained evidence verifiable. GitHub already
supports ephemeral runners; this complements it rather than inventing that
lifecycle. Build provenance describes artifact production; this receipt concerns
post-job cleanup. No research-priority or SLSA-compliance claim is established.

All 11 labeled walkthrough runs matched expected verdicts: eight clean and three
blocked. These repeated controlled scenarios do not establish a zero production
error rate. Performance must separate the deliberate observation pause from
processing overhead. Broader adversarial testing, independent trust domains,
unattended GitHub App integration and scaling remain future work.

The deployment uses Docker/kind on EC2, not Firecracker. Logs are deliberately
retained, so cleanup never means every copy of data vanished. A valid signature
can authenticate a failure receipt. An incident does not automatically imply
branch protection or deployment enforcement has been configured.

## References

- [GitHub self-hosted runner reference](https://docs.github.com/en/actions/reference/runners/self-hosted-runners)
- [SLSA build provenance](https://github.com/slsa-framework/slsa/blob/main/spec/build-provenance.md)
- [Detailed technical Q&A](outputs/TECHNICAL-PANEL-PREP.md)
- [Exact original-session walkthrough](outputs/LIVE-DEMO-WALKTHROUGH.md)
