#!/usr/bin/env python3
"""Create and prove a consistent PostgreSQL metadata backup by restoring it."""
import argparse
import datetime
import hashlib
import json
import os
import pathlib
import re
import subprocess
import uuid

ROOT = pathlib.Path(__file__).resolve().parent.parent
CONTAINER = 'cleanup-receipt-db-1'
DEFAULT = ROOT / '.build/release/backup'

def run_db(args, *, data=None, text=False):
    command = ['docker', 'exec']
    if data is not None:
        command.append('-i')
    command += ['--user', 'postgres', CONTAINER, *args]
    result = subprocess.run(command, cwd=ROOT, input=data, capture_output=True, text=text, timeout=180)
    if result.returncode:
        error = result.stderr if text else result.stderr.decode(errors='replace')
        raise RuntimeError(error[-2000:])
    return result.stdout

def rows(database, sql):
    output = run_db(['psql', '--no-psqlrc', '--tuples-only', '--no-align', '--dbname', database, '--command', sql], text=True)
    return [line for line in output.splitlines() if line]

def digest(lines):
    return hashlib.sha256(('\n'.join(lines) + '\n').encode()).hexdigest()

def snapshot(database):
    counts = json.loads(rows(database, "SELECT json_build_object('receipts',(SELECT count(*) FROM evidence.receipts),'incidents',(SELECT count(*) FROM evidence.incidents),'attempts',(SELECT count(*) FROM evidence.finalization_attempts),'schema_version',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied))::text;")[0])
    identities = {
        'receipts': digest(rows(database, "SELECT concat_ws('|',id,provider,repository_id,run_id,run_attempt,job_id,verdict) FROM evidence.receipts ORDER BY id;")),
        'incidents': digest(rows(database, "SELECT concat_ws('|',id,receipt_id,state,issue_key) FROM evidence.incidents ORDER BY id;")),
        'attempts': digest(rows(database, "SELECT concat_ws('|',id,provider,repository_id,run_id,run_attempt,job_id,attempt_number,origin,result) FROM evidence.finalization_attempts ORDER BY id;")),
    }
    return {'counts': counts, 'identity_sha256': identities}

def restore_and_verify(backup, expected):
    if hashlib.sha256(backup.read_bytes()).hexdigest() != expected['backup_sha256']:
        raise RuntimeError('Backup digest changed')
    database = 'proofrestore_' + uuid.uuid4().hex[:16]
    if not re.fullmatch(r'proofrestore_[0-9a-f]{16}', database):
        raise RuntimeError('Unsafe restore database name')
    run_db(['createdb', '--template=template0', database])
    try:
        data = backup.read_bytes()
        run_db(['pg_restore', '--exit-on-error', '--no-owner', '--no-privileges', '--dbname', database], data=data)
        restored = snapshot(database)
        if restored != expected['snapshot']:
            raise RuntimeError('Restored metadata does not match the backup manifest')
        return {
            'kind': 'metadata-restore-verification/v1',
            'checked_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
            'temporary_database': database,
            'backup_sha256': expected['backup_sha256'],
            'snapshot': restored,
            'matched': True,
            'temporary_database_removed': True,
        }
    finally:
        run_db(['dropdb', '--if-exists', database])

parser = argparse.ArgumentParser()
parser.add_argument('mode', choices=['create', 'verify'])
parser.add_argument('--directory', type=pathlib.Path, default=DEFAULT)
args = parser.parse_args()
directory = args.directory.resolve()
allowed = (ROOT / '.build').resolve()
if allowed != directory and allowed not in directory.parents:
    raise SystemExit('Backup working directory must stay under .build')
directory.mkdir(parents=True, exist_ok=True)
backup = directory / 'metadata.dump'
manifest_path = directory / 'metadata-manifest.json'

if args.mode == 'create':
    before = snapshot('proof')
    data = run_db(['pg_dump', '--format=custom', '--compress=9', '--no-owner', '--no-privileges', '--dbname', 'proof'])
    after = snapshot('proof')
    if before != after:
        raise SystemExit('Metadata changed while the release snapshot was being checked; retry')
    pending = directory / '.metadata.dump.pending'
    pending.write_bytes(data)
    os.chmod(pending, 0o600)
    os.replace(pending, backup)
    manifest = {
        'kind': 'metadata-backup/v1',
        'created_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
        'database': 'proof',
        'format': 'PostgreSQL custom archive',
        'backup_file': backup.name,
        'backup_bytes': backup.stat().st_size,
        'backup_sha256': hashlib.sha256(backup.read_bytes()).hexdigest(),
        'snapshot': before,
        'contains_secrets': False,
    }
    manifest_path.write_text(json.dumps(manifest, indent=2) + '\n')
else:
    manifest = json.loads(manifest_path.read_text())

verification = restore_and_verify(backup, manifest)
(directory / 'restore-verification.json').write_text(json.dumps(verification, indent=2) + '\n')
print(json.dumps({'backup': manifest, 'restore': verification}, indent=2))
