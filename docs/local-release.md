# Local release operations

This project is packaged for the prepared Ubuntu/WSL laptop. Docker Desktop must
be running. Startup uses only cached binaries, dependencies, pinned images,
private trust material, and persistent local volumes. It performs no package or
image download and does not require a paid account.

## Start, inspect, and stop

From the repository in WSL:

```bash
bash dev release-up
bash dev release-status
bash dev release-stop
```

`release-up` starts PostgreSQL and migrations, the preserved kind cluster and
network policy, MinIO/KES, private Sigstore, the API/dashboard, and the persistent
watchdog. It finishes only after a real archived receipt verifies through the API.

`release-stop` targets only the project's exact Compose services and the
label-checked kind control-plane container. It never removes containers, volumes,
the cluster, database rows, archived objects, signing state, or recovery ledgers.
The next `release-up` restarts the same identities.

The dashboard is [http://localhost:8080/](http://localhost:8080/). The generated
`Run-cleanup-release.cmd` beside the repository provides the same start operation
from Windows.

## Backup and proof of restore

```bash
python3 scripts/metadata-backup.py create
python3 scripts/metadata-backup.py verify
```

The backup contains the PostgreSQL database schema and sanitized metadata. It
does not contain Docker secrets, signing keys, database passwords, raw logs, or
MinIO objects. Verification creates a uniquely named temporary database, restores
the custom-format archive, compares counts and identity digests for receipts,
incidents, attempts and the Goose schema version, then drops the temporary
database. Production data is never replaced by this check.

The release dossier contains the verified backup, its SHA-256 manifest and the
restore result. Archive evidence remains protected in versioned local MinIO and
is represented by exact object references in the sample signed receipts.

## Rehearsal and retained release dossier

Each rehearsal starts from stopped services, starts the whole system offline,
restores the known-good backup into a disposable database, and runs one successful
cleanup plus one deliberately interrupted cleanup. Both are archived, signed,
ingested and independently verified; the interrupted cleanup creates exactly one
incident.

The release directory and `.tar.gz` contain:

- the exact tagged source archive;
- pinned image, trust, migration and schema hashes;
- the verified PostgreSQL metadata backup;
- both rehearsal reports;
- signed passing and failing receipt samples with bundles and API verification;
- an actual Chromium dashboard recording and two full-page screenshots;
- SHA-256 checksums for every included file.

Verify the retained directory with:

```bash
python3 scripts/release-package.py verify
```

The package intentionally excludes all private keys, passwords, bearer tokens,
Docker volumes and raw secret files. It is a release and evidence dossier for this
prepared computer, not a self-contained multi-gigabyte Docker image export.
