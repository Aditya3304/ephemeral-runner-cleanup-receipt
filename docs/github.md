# GitHub runs → signed cleanup receipts

The active workflow is `.github/workflows/cleanup-receipt-demo.yml`. The trusted
half runs as an **independent operator service**, not as a job on the runner being
cleaned. It polls GitHub every five minutes, downloads completed-attempt metadata,
job logs and the guard artifact, freezes an operator-owned snapshot, and invokes
the existing pinned finalizer and delivery containers. The API independently
verifies the signature, archive hashes and reconstructed GitHub receipt before
committing metadata. Existing receipt/incident deduplication and six-attempt
watchdog recovery apply to GitHub identities as well as local identities.

This does not require a `workflow_run` event or a functioning post hook to trigger
finalization. Failed, cancelled and missing-artifact runs are discovered from the
GitHub API. A two-minute completion grace period allows logs/artifacts to settle.
The signer never checks out the GitHub source, executes artifact contents, or
receives the GitHub API token. No inbound webhook/public API exposure is needed.

## One-time setup

Use the prepared **Linux/WSL operator host** with Docker and the existing private
archive/Sigstore/PostgreSQL/API stack. This is not a new AWS deployment, and it
does not change the private signing profile into public Sigstore signing.

1. Review, commit and push these changes to the repository's default branch.
   Commit is required by the existing attesting-image build: its embedded source
   revision must identify the actual finalizer code. Do not bypass that check.
2. On the trusted operator host, build/start the updated finalizer and API:

   ```bash
   bash dev finalizer-build
   bash dev finalizer-up
   bash scripts/finalizer-init.sh
   bash dev api-build
   bash dev api-up
   bash dev watchdog-build
   ```

   When upgrading an existing installation, `api-config.py` deliberately refuses
   a changed finalizer policy. Preserve a copy of `.build/api-config.json`, review
   the new policy in `.build/finalizer-config.json`, and explicitly update the
   API config's `policy` before repeating `api-up`. Do not rewrite historical
   receipts or reset retry state. Restart/reinstall the local watchdog too if
   used concurrently, so both profiles use the same approved images and lock.
   The API's `repository` must exactly match your GitHub `OWNER/REPO`.
   On a fresh fork installation, use `PROOF_REPOSITORY=OWNER/REPO bash dev api-up`;
   on an existing installation, explicitly review/update its repository allowlist.
3. Create a fine-grained GitHub token for **only this repository**, with **Actions:
   read** (plus mandatory Metadata read). Store it in an operator-only absolute
   file outside any job checkout/sandbox, mode `0600`. Do not put it in the demo
   job's secrets, artifact, command arguments, Git history, or JSON policy.
   Token rotation replaces that file without resetting receipt identities.
4. In GitHub Actions, find **Cleanup receipt GitHub demo**. Obtain its numeric
   workflow ID from GitHub's workflow listing (with an authenticated `gh` CLI):

   ```bash
   gh api repos/OWNER/REPO/actions/workflows --jq '.workflows[] | select(.path==".github/workflows/cleanup-receipt-demo.yml") | .id'
   ```

5. Create an operator policy such as `.build/github-policy.json`:

   ```json
   {
     "repository": "OWNER/REPO",
     "workflow_id": 12345678,
     "workflow_path": ".github/workflows/cleanup-receipt-demo.yml",
     "jobs": {"cleanup": "cleanup"},
     "since": "2026-09-08T00:00:00Z",
     "token_file": "/absolute/operator-secrets/github-actions-read.token"
   }
   ```

   Replace the repository, workflow ID, absolute token path and start date. Set
   `since` before the first run you want covered, not after it. Job mapping keys
   are YAML job IDs (`GITHUB_JOB`); values are exact GitHub REST display names.
   Keys are limited to 69 characters to avoid artifact-name truncation collisions.
   Do not rename the job or remap this policy without reviewing the consequences
   for existing identities. Matrix jobs are not supported by this simple mapping.

6. Install the approved binary and policy, then enable recovery:

   ```bash
   python3 scripts/watchdog-install.py --github .build/github-policy.json --service
   systemctl --user status cleanup-receipt-github-watchdog.service
   ```

   The installer records immutable local image IDs and hashes the installed
   binary/config files. A separate GitHub service shares the process lock with
   the local watchdog; the two must run under the same operator installation.
   Configure service-manager group membership/Docker access as described in
   [the watchdog guide](watchdog.md). Do not host untrusted CI jobs on this machine.

## Demonstrate it

Open GitHub → Actions → **Cleanup receipt GitHub demo** → **Run workflow**. Run
these scenarios in order, noting each run ID. Wait for completion plus the
two-minute grace period and the next five-minute poll, or request one cycle:

```bash
python3 scripts/watchdog-run.py --github --run RUN_ID
python3 scripts/watchdog-run.py --github status
journalctl --user -u cleanup-receipt-github-watchdog.service -n 40 --no-pager
```

| Scenario | What the audience should see |
| --- | --- |
| `normal` | Successful job, preliminary artifact, signed **partial** receipt; job-reported cleanup and trusted archived GitHub logs. |
| `command-failure` | Job exits 17, but the Action post still cleans and uploads. A signed receipt still appears. A failed build is not automatically failed cleanup. |
| `cleanup-failure` | An intentionally blocked Kubernetes resource makes cleanup time out; signed **fail** receipt and cleanup incident. The whole cluster is disposable inside this GitHub job. |
| `missing-post` | The demo intentionally skips the guard, simulating no post artifact. The external collector still creates a signed **partial** receipt with `evidence_status: missing`. This is not a test of physically killing a runner. |

Open AFTER at `http://localhost:8080/`, search the run ID, and inspect coverage,
archive references and recovery. The database provider is `github`, not `local`;
the signed `run.json` binds the numeric GitHub job ID, workflow ID/path, source
revision, run and attempt. Identity `job_id` remains the configured YAML key.
Repeat the manual watchdog cycle: it must report the same receipt UUID, not a
second receipt or incident. Use GitHub **Re-run all jobs** to demonstrate that a
new run attempt gets its own receipt without overwriting the first attempt.

For recovery, stop **only** the GitHub watchdog before dispatching a run, then
restart it after the job completes. It discovers the missed run without replaying
the job. Keep the interruption under the artifact's one-day retention if you want
the preliminary evidence included; expired evidence becomes an explicit gap.

## Honest boundaries

- `partial` is the correct successful-demo verdict here. A GitHub job cannot
  independently prove its own VM destruction. Namespace/filesystem claims from
  the Action remain `observer: job`; no job-supplied UID becomes a trusted
  Kubernetes binding. This profile cannot manufacture an all-trusted `pass`.
- Only the configured workflow and configured job keys, from `since` onward,
  are covered. Add a reviewed integration/policy for other workflows; merely
  enabling this demo does not cover every workflow in the organization.
- Discovery is paginated with an explicit 1,000-run window bound and up to 100
  attempts per run. Saturation reports an error instead of silently skipping
  pages. The available page identities are still processed, but older undiscovered
  runs require a separate backfill before moving `since` forward through a reviewed
  reinstall; durable pending identities remain eligible. This is a hackathon
  collector, not a high-volume organization-wide scheduler.
- Missing/expired/oversized logs stay unobservable. Authentication, rate-limit,
  download and network errors are retried, not mislabeled as successful cleanup.
  Input snapshots are immutable once committed, so retries never silently
  replace evidence or re-sign different inputs at the same identity.
- The implementation supports github.com, not GHES or arbitrary API/download
  hosts. Signed redirects are HTTPS-only, host-checked and carry no API token.
- There is no claim of a live GitHub/private-stack acceptance run merely because
  unit tests pass. Validate the scenarios above on your installed operator stack
  before presenting a recorded end-to-end demonstration.

API contracts: [workflow runs](https://docs.github.com/en/rest/actions/workflow-runs),
[attempt jobs and logs](https://docs.github.com/en/rest/actions/workflow-jobs),
and [artifacts](https://docs.github.com/en/rest/actions/artifacts).

## Implementation checks

On Linux, run the focused regression suites with:

```bash
go test ./internal/githubrun ./internal/finalizer ./internal/api ./internal/watchdog ./cmd/watchdog ./internal/proof ./internal/coordinator ./internal/delivery
```

The GitHub tests cover API identity and attempt binding, optional job response
fields, pagination, safe token-free redirects, ZIP limits, missing/cancelled runs,
invalid evidence, immutable publication, signing retries, signature/archive
tampering and duplicate finalization. The API boundary tests use real Ed25519
signatures as a test double for Sigstore and an in-memory archive; they do not
claim a real Fulcio/Rekor/MinIO deployment.

`TestGitHubHTTPDatabase` additionally passed against PostgreSQL 17.11 in a fresh,
network-isolated Docker container with a tmpfs database and the restricted
`proof_api` role. It exercised the real HTTP handler, schema migrations, receipt
and incident commit, duplicate ingestion, dashboard lookup and recovery linkage.
It is opt-in (`PROOF_GITHUB_DISPOSABLE_DB=1`) and expects a throwaway trust-auth
PostgreSQL server at loopback with a `proof_api` login. Never point this test at
the operator's database. Live GitHub dispatch remains the acceptance step above.
