# Understand and demonstrate the AWS cleanup proof

Use this as a rehearsal, then repeat the same experiment live. You do not need
to invent an impressive application. You need to show a real workload, change
one condition, and inspect independently verifiable evidence of the outcome.

The application added for this demonstration processes three **synthetic** orders.
It calculates `2 × 1250 + 1 × 999 + 3 × 500 = 4999` cents, writes input/output
files, creates a dummy credential file, and runs an assertion. There is no real
customer information or usable secret. This is a realistic CI data-processing
pattern, not a claim of production customer adoption.

All three cases have now been executed on AWS. See the
[measured results and direct run links](CONTROLLED-EXPERIMENTS.md), then follow
this guide to reproduce them yourself.

The workload is in `examples/aws-demo/order-export.mjs`. Its arithmetic tests
are in `examples/aws-demo/order-export.test.mjs`. The workflow is
`.github/workflows/cleanup-aws.yml`. The deployed source pin for these experiments
is `9605c536dd49faeb3acf6a80a60ec1136ad604c0`.

## 1. Understand the question being answered

**GitHub answers:** did the workflow steps succeed?

**Your system answers:** after the runner stopped, were the defined cleanup
conditions independently observed, and can the signed record be verified?

These are separate questions. A failed test can still clean up correctly. A
successful business calculation does not establish that its files were removed.
The GitHub listener itself can exit normally even when a workflow step fails,
so a Kubernetes `Succeeded` pod is not the application's test result. Read the
GitHub job/step conclusion for that result.
The observer inspects before Kubernetes disposes of the runner. Otherwise,
deleting the whole pod could conceal a failed in-runner cleanup attempt.

| Experiment | Deliberate change | Expected workflow | Expected cleanup |
| --- | --- | --- | --- |
| `normal` | Correct expected total: 4999 | Success | Pass |
| `test-failure` | Incorrect expected total: 5000 | Failure | Pass, if all cleanup checks succeed |
| `cleanup-blocked` | Leave synthetic residue inside a child directory with mode 0500 | Failure from the cleanup post step, although calculation passes | Fail for workspace residue |

These are expectations, not hardcoded receipt verdicts. Read the resulting
signed receipt. Unavailable observation can instead produce partial coverage;
never describe that as a pass. The controlled obstruction only affects a newly
created directory inside that job's disposable workspace. It is not an attack
against the laptop or host filesystem.

## 2. Know where every component runs

| Component | Location | Job |
| --- | --- | --- |
| GitHub Actions | GitHub | Queues the workflow and records step results |
| Restricted GitHub bridge | Your laptop | Uses your local GitHub login for allowlisted registration/metadata/status operations |
| SSM tunnels | Laptop ↔ AWS | Encrypt the bridge and private dashboard connections; no public inbound ports |
| Coordinator | EC2 host service | Pins source/run/attempt/UIDs; creates the runner; coordinates observation and disposal |
| kind | Docker container on EC2 | Supplies the Kubernetes node and API |
| Runner | Temporary Kubernetes pod | Executes the real GitHub job using one-use JIT registration |
| Native action guard | Inside the runner | Creates the scoped workspace and attempts cleanup in its post step |
| External observer | Trusted process inside the kind node, outside the runner | Watches runtime logs before job start; inspects the pinned filesystem after termination |
| Finalizer | Separate short-lived Docker container on EC2 | Validates observations, archives evidence and signs a receipt |
| Issuer / Fulcio | Separate trusted containers on EC2 | Authenticate the finalizer and issue its short-lived signing certificate |
| CTLog / Rekor / TSA | Separate trusted containers on EC2 | Certificate transparency, signature transparency and timestamp evidence |
| S3 / KMS | AWS managed services | Versioned, encrypted, retention-protected evidence objects |
| Receipt API / PostgreSQL / dashboard | Containers on EC2 | Verify and accept receipts, store searchable metadata and display it |

All host-side components share **one EC2 instance**. Separation is by processes,
containers, identities, mounts and network policy. This is not independent
physical infrastructure, and this deployment does not use Firecracker.

## 3. Start from the prepared infrastructure

Open Ubuntu/WSL on your laptop. Commands marked **LAPTOP** run there; commands
marked **AWS HOST** run only after opening an SSM shell. Do not mix the two.

**LAPTOP — begin every new laptop terminal here:**

```bash
cd /mnt/c/Users/Aditya/Documents/Codex/2026-09-08/i-have-a-hackathon-finals-round
```

Check the instance:

```bash
bash work/aws.sh ec2 describe-instances \
  --instance-ids i-0f21b25d3a04d8541 \
  --query 'Reservations[0].Instances[0].State.Name' --output text
```

If it says `stopped`, start it and wait:

```bash
bash work/aws.sh ec2 start-instances --instance-ids i-0f21b25d3a04d8541
bash work/aws.sh ec2 wait instance-running --instance-ids i-0f21b25d3a04d8541
```

If authentication expired, use `bash work/aws.sh login` and complete the browser
authorization. Do not display credentials. Allow a few minutes for Systems
Manager and the prepared services to recover after starting EC2.

There are three persistent laptop processes. They were started during setup;
do not start duplicates if their ports are already in use. To reconnect after
closing them, run each in its own WSL terminal:

```bash
# Terminal 1: bridge tunnel. Keep running.
bash work/tunnel-bridge.sh
```

```bash
# Terminal 2: GitHub bridge. Keep running.
python3 outputs/ephemeral-runner-cleanup-receipt-aws/scripts/aws/github-bridge.py \
  --revision 9605c536dd49faeb3acf6a80a60ec1136ad604c0
```

```bash
# Terminal 3: dashboard tunnel. Keep running.
bash work/tunnel-dashboard.sh
```

Open [the private dashboard](http://localhost:18080). Readiness can be checked with:

```bash
curl --fail http://localhost:18080/readyz
```

Expected: `{"status":"ready"}`. An empty dashboard is different from a failed
connection; inspect readiness first. These helpers use task-local AWS login
files and downloaded CLI/plugin binaries. If WSL `/tmp` tools were removed,
restore/install AWS CLI v2 and the Session Manager plugin as described in the
[setup guide](FINALS-DEMO.md); closing a tunnel is not a reason to redeploy AWS.

## 4. Open the background views before starting the job

**LAPTOP — open an interactive AWS shell in a new terminal:**

```bash
export PATH=/tmp/cleanup-ssm/usr/local/sessionmanagerplugin/bin:$PATH
bash work/aws.sh ssm start-session --target i-0f21b25d3a04d8541
```

**AWS HOST — initialize every new host shell:**

```bash
sudo -i
cd /opt/cleanup/repo
export KUBECONFIG=/opt/cleanup/repo/.build/kubeconfig
```

Show the running source and services:

```bash
git rev-parse HEAD
systemctl is-active cleanup-stack cleanup-github cleanup-github-proxy cleanup-github-broker cleanup-metadata-block
docker ps --format '{{.Names}}  {{.Status}}'
kubectl get nodes
```

Expected: the pinned revision above; five `active` results; database, signing,
API and kind containers; a `Ready` node. No MinIO or KES server runs here. You
will not see a permanent finalizer container: it runs only during finalization.

Prepare these views in separate AWS shells, or switch between them:

```bash
# A: watch actual pod creation and termination.
kubectl get pods -A --watch
```

```bash
# B: show changes from the actual durable coordinator ledger.
python3 scripts/aws/watch-demo.py
```

```bash
# C: watch finalization/publication output.
journalctl -u cleanup-github.service -f -n 15 --no-pager
```

```bash
# D, optional: see the short-lived finalizer start and stop.
docker events --filter type=container --filter event=start --filter event=die \
  --format '{{.Time}} {{.Action}} {{.Actor.Attributes.name}}'
```

The ledger viewer is read-only. It prints selected identity, UID, phase, log
coverage and disposal fields; it does not produce the verdict. Very short phases
can occur between polls. Missing values are unknown. `phase: complete` means
coordinator completion, not yet signature acceptance.

Do not dump container environment variables, Kubernetes Secrets, STS session
files, signing volumes or JIT registration config onto a projector. None is
needed to prove the lifecycle.

## 5. Trigger the first experiment

**LAPTOP — from the task folder:**

```bash
gh api --method POST \
  repos/Aditya3304/ephemeral-runner-cleanup-receipt/actions/workflows/cleanup-aws.yml/dispatches \
  -f ref=aws-github-finals \
  -f 'inputs[scenario]=normal' \
  -f 'inputs[observation_seconds]=45'
```

No response body on success is normal. Find the new run:

```bash
gh run list --repo Aditya3304/ephemeral-runner-cleanup-receipt \
  --workflow cleanup-aws.yml --limit 5
```

Open its link on GitHub. The feature-branch workflow may not expose a convenient
Run workflow button while the pull request is unmerged; the dispatch API is the
tested route. The job's unique label is `proof-gh-RUN-ATTEMPT`. A queued job does
not execute until the coordinator admits that exact source and registers a runner.

The prepared source must still be the approved branch HEAD. After adding code or
changing the workflow, redeploy the committed revision and update the bridge pin
before dispatching it. An old run's **Re-run all jobs** preserves its source and
inputs; it does not test a newer edit.

## 6. Explain what happens while it runs

1. GitHub queues the job. The coordinator checks repository, branch, workflow,
   exact commit and job identity through the restricted laptop bridge.
2. A one-use GitHub runner registration is created. The coordinator writes its
   durable run ledger and creates UID-bound namespaces, RBAC and a pod.
3. The external observer arms its log watch **before** the network gate releases
   the runner. The watch covers the guard container's runtime stdout/stderr.
4. The real GitHub runner starts. Native guard main creates the disposable
   workspace and credential directory. The job checks out the approved source.
5. The workload writes synthetic files, computes 4999 cents and pauses 45 seconds.
6. GitHub runs native action post even when the application assertion fails.
   The guard attempts scoped filesystem and Kubernetes cleanup. The launcher
   attempts removal of the disposable GitHub runtime, then terminates.
7. The observer checks the stopped container's pinned volume and log continuity.
   The coordinator checks Kubernetes disposal and GitHub assignment/registration
   absence. A later pod deletion is not substituted for the earlier observation.
8. Immutable observations and a finalization-ready marker bind the exact input
   hashes. The isolated finalizer validates them, archives evidence and signs.
9. The API independently verifies policy, signature and S3 object references
   before storing the receipt in PostgreSQL. Only then does the coordinator post
   the separate `cleanup/signed-receipt` GitHub status.

During the pause, copy `token` and `pod_name` from the ledger viewer. In another
**AWS HOST** shell, replace the two example values:

```bash
TOKEN=PASTE_THE_32_CHARACTER_TOKEN
POD=PASTE_THE_POD_NAME
kubectl exec -n "proof-runner-$TOKEN" "$POD" -c guard -- \
  ls -la "/data/proof-$TOKEN/workspace"
kubectl exec -n "proof-runner-$TOKEN" "$POD" -c guard -- \
  cat "/data/proof-$TOKEN/workspace/export-summary.json"
kubectl exec -n "proof-runner-$TOKEN" "$POD" -c guard -- \
  ls -la "/data/proof-$TOKEN/credentials"
```

You should see synthetic input/output and credential filenames; the summary is
three orders and 4999 cents. These commands are read-only. If the pod has already
terminated, exec should fail; do not call that a bug or recreate it to fake a view.
Repeat the experiment and inspect during the announced pause.

## 7. Inspect evidence after termination

Keep `TOKEN` set in the **AWS HOST** shell:

```bash
python3 -m json.tool ".build/coordinator/$TOKEN/collector.json"
python3 -m json.tool ".build/coordinator/$TOKEN/observations.json"
python3 -m json.tool ".build/coordinator/$TOKEN/finalization-ready.json"
```

Point to the collector's `armed_at`, start/termination flags, pinned pod/container
identities, log digest, and workspace finding. Then point to the derived five
coverage findings. The ready marker commits to ledger and observation hashes.
These host records are inputs to signing; by themselves they are not signatures.

Compare the ready marker's two digests with the actual files:

```bash
sha256sum ".build/coordinator/$TOKEN/run.json" ".build/coordinator/$TOKEN/observations.json"
```

Inspect current disposal separately:

```bash
kubectl get namespaces
```

**LAPTOP:**

```bash
gh api repos/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runners \
  --jq '{total_count, runners: [.runners[] | {id,name,status,busy}]}'
```

Expected after completion: no namespaces for that token and no runner registration
for that run. Other Kubernetes system namespaces should remain.

In the dashboard, select the receipt matching the exact run and attempt. Compare
its source SHA, verdict, five coverage fields, signature state and archive refs.
Inspect metadata directly on the **AWS HOST**:

```bash
docker exec --user postgres cleanup-receipt-db-1 \
  psql --no-psqlrc -d proof -c \
  'SELECT run_id,run_attempt,verdict FROM evidence.receipts ORDER BY run_id DESC,run_attempt DESC LIMIT 10;'
```

The database is an index of accepted records, not the proof by itself.

## 8. Show actual S3 and cryptographic verification

In the AWS console, open S3 bucket `cleanup-evidence-385020093237-us-east-1`.
Select the key **and exact version** from the receipt's archive reference. Show
SSE-KMS encryption, version ID and Compliance retention. Do not use an arbitrary
similarly named object. Version IDs and hashes bind the specific bytes.

To export the latest receipts and verify on this prepared laptop:

```bash
python3 work/export-evidence.py
python3 work/verify-offline.py
```

The export downloads exact S3 versions and checks their hashes. The verification
uses the pinned Cosign binary and public private-Sigstore trust root. It also
checks that appending a byte causes rejection. Original receipt files remain
unchanged. The [evidence README](aws-validation/README.md) contains the standalone
Cosign command, so a judge can verify independently with the public files.

Explain the distinction: a hash detects different bytes when you trust the
expected hash; a signature binds those bytes to the trusted signing identity.
Neither makes a dishonest host truthful. A valid signature on a `fail` receipt
authenticates the failure record; it is not a successful cleanup.

For the blocked case, the final receipt conservatively preserves the job's
`workspace: failed` claim with `observer: job`. Do not call that field an external
observation. Open the archived `observations.json` referenced by the signed
receipt: its `collector.workspace` separately records the trusted observer's
residual finding. Its exact bytes and hash are exported in the experiment packet.

## 9. Repeat the two negative cases

Wait for the current signed receipt, then repeat the dispatch command from
section 5, replacing only `inputs[scenario]` with `test-failure`. Watch the
assertion compare 4999 with the deliberately wrong expectation 5000. GitHub should
fail, while correctly observed cleanup should still pass.

Repeat with `cleanup-blocked`. The application calculation should pass; guard
post should report inability to remove the protected synthetic child. Show the
observer's residual-workspace finding and signed failure. Kubernetes can still
dispose of the runner afterward; that does not erase the recorded failure at the
earlier observation boundary.

Allow extra time for the deliberately blocked case: the configured guard cleanup
budget is 120 seconds. Show its post step while it attempts cleanup; do not cancel
the job merely because the negative case takes longer than the normal case.

The GitHub commit status context is shared by runs of the same commit, so its
latest value can change as you run these cases. Use the receipt's **run ID and
attempt** for historical comparisons. Finish the rehearsal with a normal case
if you want the latest status to show a pass, while retaining negative receipts.

## 10. Be prepared for questions

**“Could the job simply say it cleaned up?”** It can supply preliminary evidence,
but that input remains untrusted. Filesystem and log observations come from the
node observer; disposal checks come from the coordinator.

**“What is actually signed?”** The structured receipt binds the run/source,
coverage findings, trusted finalizer identity and content-addressed versioned
archive references. The signature is not a claim that every physical byte was erased.

**“Why so many signing services?”** Fulcio binds an ephemeral key to the private
finalizer identity; CTLog records certificate transparency; Rekor supplies
signature transparency evidence; TSA supplies signed timestamp evidence. Their
public trust material lets an offline verifier check the bundle. AWS KMS encrypts
the archive; it is not the finalizer's Sigstore signing key.

**“What do credentials/resources/logs mean?”** Credentials means the scoped
credential files and run RBAC, not external token revocation. Resources means
the assigned Kubernetes namespace in this restricted runner profile, not all
AWS resources. Logs means bounded CRI stdout/stderr from the watched runner
container, not every possible file or service log.

**“Can a malicious EC2 administrator forge the observation?”** The operator,
node/runtime and private signing infrastructure are trusted. This demo does not
defend against their compromise. Do not claim hardware attestation or microVM
destruction. AWS EC2 is real; Firecracker is not part of this deployment.

**“How do I know this was implemented rather than animated?”** Choose an input
live; correlate the fresh GitHub run/attempt with Kubernetes UIDs, host observation
hashes, exact S3 versions and an independently verified signature. Then introduce
the controlled fault and show the changed result. Source, automated checks and
negative cases are reviewable in the pull request. No demo proves who authored
every line or that the system is production-ready.

## 11. Stop and restart, or rebuild from nothing

After the final receipt arrives, stop EC2 using the command in
[FINALS-DEMO.md](FINALS-DEMO.md). Closing the browser does not stop billing.
The eight-hour timer runs per host boot; check it on the host with
`systemctl list-timers cleanup-demo-stop.timer --all`. EBS/S3/KMS remain after
stopping, and Compliance-locked evidence cannot be deleted early.

A normal live demo starts the **prepared host** and creates a brand-new runner.
It need not destroy and recreate the trusted signing infrastructure each time.
For a true infrastructure rebuild, use the committed `infra/aws/template.py`
and `scripts/aws/deploy.py` from the isolated checkout. First export existing
evidence and metadata and follow the staged teardown guide; bucket names and
locked retained evidence require deliberate reconciliation, not blind deletion.
With an authenticated standard AWS CLI profile, `deploy.py --profile PROFILE`
generates a reviewable template; adding `--apply` provisions/deploys the committed
source. Update the local bridge pin to that deployed SHA. The full resource order,
IAM and cost explanation is in
[the AWS guide](ephemeral-runner-cleanup-receipt-aws/docs/aws-finals.md).

## 12. If something does not match the expectation

| Symptom | Check |
| --- | --- |
| Only the first normal case appears | Inspect the terminal for an interrupted command. The local walkthrough helper now retries temporary GitHub/API read failures; job dispatch is never automatically retried. All scenarios share the job name `guarded`, so distinguish them by run ID and signed verdict. |
| Job remains queued | EC2 running, controller active, bridge/tunnel alive, exact source SHA admitted |
| Port already in use | Existing tunnel may already be running; do not launch duplicates |
| Dashboard unavailable | Dashboard tunnel, `/readyz`, then API and database services |
| Workflow finished but receipt absent | Wait for collection/finalization; inspect controller journal and ready marker |
| Partial receipt | Read the missing observation reason; do not relabel it a pass |
| Cleanup failure in the blocked case | Expected; compare workspace residual with the other verified checks |
| Old run rejected after source upgrade | Use the currently deployed SHA; archived old receipts remain historical records |
| S3 deletion denied | Check role restrictions and Object Lock deadlines; denial is expected in those cases |

Rehearse normal → test-failure → cleanup-blocked → normal. Start the background
watches before dispatch. Describe what the evidence says, including its limits.
