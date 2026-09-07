# Local recovery watchdog

The watchdog finishes missing receipts for real local Kubernetes runs. It runs as
`cleanup-receipt-watchdog.service` in Ubuntu's user service manager. It checks at
startup, then waits five minutes after each cycle. WSL, Docker Desktop and the
project's local services must be running. User lingering is enabled on this laptop,
so closing a terminal does not stop the service; shutting down WSL or Docker does.

## Use the prepared laptop

Open AFTER at http://localhost:8080/ and expand **Recovery watch**. Press **Refresh**
to reload recorded recovery states. “Recovered” means the API has independently
verified and stored a receipt; the receipt can still contain partial or failed
cleanup coverage. Recovery records are operational metadata, not signed verdicts.

Run these commands from this repository in Ubuntu:

```bash
bash dev watchdog-status
systemctl --user status cleanup-receipt-watchdog.service
journalctl --user -u cleanup-receipt-watchdog.service -n 40 --no-pager
bash dev watchdog-stop
systemctl --user start cleanup-receipt-watchdog.service
```

The status command reads durable local files and works even when the API or
PostgreSQL is down. It lists all current run states; complete per-attempt history
is retained in each state file. The dashboard lists the latest attempt per run,
up to 100, with unresolved work first. Neither view is a live daemon heartbeat.

To start all project dependencies after a laptop restart:

```bash
bash dev ci-up
bash dev finalizer-up
bash dev dashboard-up
systemctl --user start cleanup-receipt-watchdog.service
```

A prepared Windows shortcut, `Run-cleanup-watchdog.cmd`, performs this startup.
It uses cached assets and preserves installed identities, evidence and data.

## Install an intentional operator update

```bash
bash dev watchdog-build
bash dev watchdog-install
```

Installation copies the compiled worker and approved finalizer configuration into
a content-addressed directory. It pins the API and finalizer by exact local image
digest and retains dedicated image tags so a later development build cannot make
the installed manifest disappear. Every cycle verifies installed file hashes.
The runtime never invokes checkout scripts or a command copied from a job ledger.
Rebuilding source alone does not change the installed service.

The installer checks Docker access from a user service before changing its unit.
If terminal Docker works but this check fails after Docker group setup, the WSL
user manager may have stale supplementary groups. Refreshing that manager with
`sudo systemctl restart user@1000.service` fixed this laptop's installation.
That restarts this user's WSL services, including desktop portals. On another
machine, use its actual user ID. No socket permissions were relaxed.
User lingering is enabled here; on a fresh installation use
`sudo loginctl enable-linger "$USER"` to retain services after terminal logout.

The watchdog uses the existing approved finalizer image and private signing
policy. It does not rebuild or loosen that trust boundary. API authentication
stays in the Docker auth volume and is mounted only into isolated bridge/delivery
containers. Public metadata never includes its bearer token or signing keys.

## Retry and crash behavior

- A process-owned filesystem lock prevents overlapping workers. A shared
  coordinator lock prevents recovery from racing an active CI coordinator. On
  this single laptop, a backlog sweep can temporarily delay a new CI run.
- A started run becomes eligible after its configured timeout plus one minute;
  completed runs can be reconciled immediately. Recovery resumes UID-bound
  collection. It never launches a job that had not started or reruns its command.
- Before work, the worker atomically writes and synchronizes its attempt, lease
  and crash retry deadline. A killed worker consumes that attempt. An expired
  descriptive lease cannot steal the lock from a still-running worker.
- There are at most six recovery dispatches per identity. Delays after failures
  are 1, 5, 15, 60 and 60 minutes, checked on the next cycle. Existing finalizer
  and delivery budgets and later retry deadlines are preserved.
- A verified API receipt with the exact published receipt and bundle references
  is reconciled before retry deadlines or exhaustion. This recovers a lost
  response without signing again or inserting a second receipt.
- Completed runs do no further cleanup or signing. Pending attempt reports are
  replayed to the API idempotently after an outage.
- Orphan Docker children bearing this project's explicit watchdog label are
  removed only after the next worker owns the lock.
- Invalid durable state fails closed. Incomplete historical canonical input or
  broken readiness bindings are retained as `invalid_evidence`, with automatic
  dispatch stopped. Exhausted underlying stages are `stage_exhausted`.
  No reset or evidence-rewriting command is provided.

Migration 00002 distinguishes delivery attempts from watchdog attempts, so each
can retain its original numbering. Authenticated recovery updates can link success
only to an already verified receipt for the same identity. Old progress cannot
downgrade a completed attempt. Migration rollback refuses to discard recorded
watchdog history.

## Validation and remaining scope

See [watchdog-validation.md](watchdog-validation.md) for measured outcomes.
The two controlled recovery demos create real jobs:

```bash
bash dev watchdog-stop
python3 scripts/demo-watchdog-outage.py
python3 scripts/demo-watchdog-orphan.py
systemctl --user start cleanup-receipt-watchdog.service
```

Run the final start command even if a demo fails. The outage script restores the
API in a finally block. Each demo refuses to run while the service is active to
avoid competing with its deliberately controlled retry schedule.

This is a local WSL service, replacing the plan's hosted scheduler under the
approved offline scope. It is not a distributed database lease scheduler.
No public Sigstore, hosted GitHub job or AWS service is used. Recovery exhaustion
is shown as an operational issue; cleanup incidents are created only from verified
failed or partial receipts through the existing ingestion transaction.

The complete fault-injection and externally blocked acceptance matrix is milestone
h. Local backup, startup packaging hardening and rehearsal are milestone i.
Neither is claimed complete by these recovery-specific checks.
