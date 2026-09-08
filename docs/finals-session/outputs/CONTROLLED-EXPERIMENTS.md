# Measured AWS demonstration results

The workload and workflow at commit
`9605c536dd49faeb3acf6a80a60ec1136ad604c0` were executed on the real AWS-hosted
ephemeral GitHub runner on 8 September 2026. These outcomes were measured, not
mocked or assigned by the demonstration script.

| Test case | GitHub result | Signed cleanup result | Execution |
| --- | --- | --- | --- |
| Correct order-export assertion | Success | Pass | [34203311613, attempt 1](https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runs/34203311613/attempts/1) |
| Deliberately wrong expected total | Application step failed; native cleanup post succeeded | Pass | [34204207125](https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runs/34204207125) |
| Deliberately non-writable child containing residue | Application step succeeded; native cleanup post failed | Fail | [34204363463](https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runs/34204363463) |
| Fresh normal rerun after the obstruction case | Success | Pass | [34203311613, attempt 2](https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runs/34203311613/attempts/2) |

The external collector for the obstruction case recorded:

> Residual directory entry remains after guard termination

It also recorded that start and termination were proven, credential files were
empty and the watched CRI logs had no rotation or watch gap. The coordinator
confirmed resource and runner disposal. These are distinct observations: eventual
Kubernetes deletion did not erase the recorded workspace-cleanup failure.

The final receipt preserves the job's own negative workspace claim with
`observer: job`. The independently obtained `observations.json`, whose exact
S3 version and hash are bound by the signed receipt, separately contains the
trusted collector's residual finding. Present both; do not mislabel the job
claim as the independent finding.

The synthetic files were also inspected through a read-only command during the
live 45-second pause. Their summary showed three orders totaling 4999 cents,
and the protected child directory was present with mode 0500. This is recorded
in [the live inspection snapshot](aws-validation/live-synthetic-files.txt).

The packet includes [machine-readable experiment results](aws-validation/controlled-experiments.json),
exact receipt/bundle and observation bytes, GitHub job snapshots and
[offline signature/tamper checks](aws-validation/offline-verification.json).
The post-experiment [GitHub runner registry](aws-validation/experiment-runner-registry.json)
was empty. The original partial receipt is retained alongside the newer records.

This demonstrates implemented integration, observable behavior, negative-case
handling and integrity verification. It does not prove production adoption,
physical erasure, hardware attestation or protection from a malicious trusted
host administrator. It also cannot establish who authored every line of code.

Use [the complete walkthrough](LIVE-DEMO-WALKTHROUGH.md) to reproduce the cases
and understand each background component.
