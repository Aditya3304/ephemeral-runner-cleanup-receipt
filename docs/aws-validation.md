# Live AWS and GitHub validation — 8 September 2026

The deployed implementation is commit `1f48c97ea421f8814a0222f66449b227779afa75`,
based exclusively on committed baseline `4bc28ba15cb6ec85fec947ef73f231611c78b63c`.
Subsequent documentation commits do not change the running source pin.
The original laptop checkout and its uncommitted changes were not used.

## Observed results

| GitHub execution | Workflow | Signed cleanup | Purpose |
| --- | --- | --- | --- |
| [34197128621, attempt 1](https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runs/34197128621) | Failed | Partial | Initial Kubernetes API proxy routing failure left workspace observation incomplete. The signed receipt records this limitation. |
| [34199257494, attempt 1](https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runs/34199257494) | Success | Pass | Corrected native GitHub runner lifecycle completed. |
| [34200085381, attempt 1](https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runs/34200085381/attempts/1) | Success | Pass | Final implementation revision completed all five checks. |
| [34200085381, attempt 2](https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runs/34200085381/attempts/2) | Success | Pass | Fresh single-job registration and cleanup after an actual EC2 restart. |

The final rerun was accepted at 07:55:51 UTC. Its receipt ID is
`8781a54f-61d1-4ceb-8c71-85f62af5e564`, with receipt SHA-256
`f7168621546b318d13cf57a1cd9290b901d9692612617eb115cc987383e5f5ad`.
The accepted receipt verifies workspace, credentials, resources, logs and runner
disposal. GitHub's separate `cleanup/signed-receipt` status is success. No demo
Kubernetes namespaces or registered GitHub runners remained after completion.

All four receipts and their signature bundles were downloaded using their exact
S3 version IDs and checked against the API's object hashes. Cosign verified all
four signatures offline against the exported public trust root. Appending one
byte to each receipt caused signature verification to fail. Signature validity
does not turn the deliberately preserved partial receipt into a passing receipt.

## Boundary and recovery checks

- Actual S3 calls using the API reader session could not write evidence:
  `AccessDenied`. Calls using the finalizer writer session could not delete
  evidence: `AccessDenied`.
- A container on the host's kind network could not contact EC2 instance metadata.
  The same checks passed after restarting the host. Private session files had
  mode 0600; rotated STS session expirations were observed.
- A PostgreSQL metadata backup was restored into a temporary database. Counts
  and identity hashes matched for three receipts, one incident and one delivery
  attempt, and the temporary database was removed. This backup precedes the
  fourth receipt from the restart rerun.
- The CloudFormation update completed successfully, preserving the EC2 instance
  and root volume. The prepared stack, GitHub controller, broker, proxy and
  metadata firewall services resumed after the actual host restart. The API
  readiness endpoint passed, and the subsequent GitHub job passed.
- Go package tests under `internal/...` and `cmd/...` passed. Focused checks also
  covered rotating archive sessions, GitHub identity admission and delivery
  retry preservation. Python bridge allowlist tests passed.

The exported validation packet contains the exact receipt bytes, signature
bundles, public trust, API records, GitHub run/job/status snapshots, offline
verification results, AWS boundary results and backup/restore report. It contains
no AWS credentials, GitHub administrative token, private signing keys or raw job
log exports. It is a validation snapshot, not a fresh online verification of
the archive's current retention state.

## Demo and limitations

Rerun the validated GitHub execution to use the approved source revision:

```bash
gh run rerun 34200085381 --repo Aditya3304/ephemeral-runner-cleanup-receipt
```

Keep the local GitHub bridge and SSM tunnels running. Open the private dashboard
at `http://localhost:18080`. Wait for the signed receipt after GitHub finishes;
workflow success alone is not a cleanup verdict. The controller intentionally
rejects an unapproved source revision. Redeploy and update the bridge's exact
revision pin before dispatching a newer branch HEAD.

This is a credit-funded EC2 deployment with process/container separation on one
host. It does not run Firecracker. No physical erasure or malicious-host defense
is claimed. Private Sigstore trust and the operator laptop bridge are explicit
dependencies. See [the deployment guide](aws-finals.md) for IAM, costs, resource
order, full teardown and exact judge talking points.
