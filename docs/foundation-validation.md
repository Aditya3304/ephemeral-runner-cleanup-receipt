# Milestone a — validation record

Validated on September 7, 2026 (Asia/Kolkata) in Ubuntu under WSL 2, using Docker
Desktop. PostgreSQL was actually started and queried; these are execution results,
not a planned checklist.

## Result

**PASS: repository and PostgreSQL foundation.** Application cleanup, signing,
ingestion and dashboard milestones have not started.

| Check | Observed result |
|---|---|
| PostgreSQL startup | 17.11, official image pinned by SHA-256 digest |
| Client transport | TLS 1.3; CA and hostname verification enabled |
| Database exposure | Internal Docker network; no host-published database port |
| Migration lifecycle | Empty DB up; second up no-op; down; up again — passed |
| Application tables | Exactly receipts, incidents and finalization_attempts in the evidence schema |
| Unique job identity | Duplicate rejected; two concurrent deliveries commit exactly one row |
| Receipt/incident pair | Fail and partial cannot commit alone; one paired incident commits; duplicate incident rejected |
| Forced failure rollback | Invalid incident aborts transaction and leaves no receipt |
| Coverage and references | Unknown/untrusted coverage, malformed references, empty pass logs and unverified signature-state rejected |
| Restricted API role | Cannot create tables/users, change migration history, update/delete receipts, delete incidents or move an incident to another receipt |
| Invalid connections | Plaintext, wrong CA, wrong hostname and wrong password all rejected |
| Search and indexes | Text search returns expected isolated fixtures; seven required query/retry indexes found |
| Retry/lease metadata | Invalid lease and retry states rejected; attempt cannot link to a different job's receipt |
| Test cleanup | Randomly named temporary test database dropped after tests |
| Interactive schema demo | Synthetic receipt plus incident validated inside a transaction, then both rolled back |
| Stop/start | Existing credentials and certificate preserved; database still at migration version 1 |
| Main database | Zero receipts, zero incidents and zero finalization attempts after verification |
| Static checks | Go vet, shell syntax and Compose configuration validation passed |

The Go integration suite passed all ten scenario groups, including four nested
invalid-connection cases. Fixtures are intentionally synthetic and isolated.
This is not a signature verification or real cleanup test.

## Run it again

From this repository in WSL:

```bash
bash dev up
bash dev check
bash dev demo
bash dev status
```

The pinned image and dependencies have already been downloaded on the development
laptop. On another machine, run `bash dev prepare` while online first. No running
database command pulls images or downloads dependencies.

## Pending and excluded

Next: milestone b, Go/Cobra/client-go cleanup CLI and real kind resource tests.
MinIO, local Sigstore, guard/coordinator, API, dashboard, watchdog and the full
offline fault matrix remain pending. GitHub-hosted execution/public Sigstore and
AWS deployments remain excluded from live validation under the approved scope.

The Node workspace files are scaffolds; Node 24 installation and actual frontend/
Action builds will be handled when those milestones begin. No hosted workflow was
created or enabled during this milestone.
