#!/usr/bin/env python3
"""Run one full stopped-to-verified local release rehearsal."""
import argparse
import datetime
import json
import pathlib
import subprocess
import time

from hardening_lib import ROOT, deliver, parse_delivery, receipt_for, verify_receipt

parser = argparse.ArgumentParser()
parser.add_argument('--number', type=int, choices=[1, 2], required=True)
args = parser.parse_args()
output = ROOT / '.build/release/rehearsals'
output.mkdir(parents=True, exist_ok=True)
target = output / f'rehearsal-{args.number}.json'
if target.exists():
    raise SystemExit(f'Rehearsal {args.number} already has a retained result; do not overwrite it')

def run(command, timeout=900, log=None):
    started = time.monotonic()
    result = subprocess.run(command, cwd=ROOT, capture_output=True, text=True, timeout=timeout)
    if log:
        log.write_text(result.stdout + result.stderr)
    if result.returncode:
        raise RuntimeError(f"Command failed: {' '.join(command)}\n{result.stdout[-1200:]}\n{result.stderr[-1200:]}")
    return round(time.monotonic() - started, 3)

started_at = datetime.datetime.now(datetime.timezone.utc).isoformat()
timings = {}
timings['stop'] = run(['bash', 'scripts/release-stop.sh'], timeout=300, log=output / f'rehearsal-{args.number}-stop.log')
timings['start'] = run(['bash', 'scripts/release-up.sh'], timeout=900, log=output / f'rehearsal-{args.number}-start.log')
run(['systemctl', '--user', 'stop', 'cleanup-receipt-watchdog.service'], timeout=60)

try:
    timings['backup_restore'] = run(['python3', 'scripts/metadata-backup.py', 'verify'], timeout=300, log=output / f'rehearsal-{args.number}-restore.log')
    before = set((ROOT / '.build').glob('finalizer-validation-*.json'))
    timings['demo'] = run(['python3', 'scripts/demo-finalizer.py', 'pass', 'missing-post'], timeout=1200, log=output / f'rehearsal-{args.number}-demo.log')
    created = set((ROOT / '.build').glob('finalizer-validation-*.json')) - before
    if len(created) != 1:
        raise RuntimeError('Expected one fresh finalizer report')
    source = json.loads(next(iter(created)).read_text())
    results = []
    for item in source['results']:
        run_id = item['run_id']
        delivered = parse_delivery(deliver(run_id))
        row = receipt_for(run_id)
        verified = verify_receipt(row['id'])
        ledger = json.loads((ROOT / '.build/coordinator' / run_id / 'run.json').read_text())
        expected = 'fail' if item['case'] == 'missing-post' else 'pass'
        if row['verdict'] != expected or delivered['receipt_id'] != row['id']:
            raise RuntimeError('Rehearsal receipt outcome differed from the expected cleanup verdict')
        if not ledger['resource_absent'] or not ledger['runner_absent']:
            raise RuntimeError('Rehearsal resources were not proven absent')
        if (row['incident'] is None) != (expected == 'pass'):
            raise RuntimeError('Rehearsal incident pairing is incorrect')
        results.append({
            'case': item['case'],
            'run_id': run_id,
            'receipt_id': row['id'],
            'verdict': row['verdict'],
            'incident_id': row['incident']['id'] if row['incident'] else None,
            'signature': verified['signature'],
            'artifacts': verified['artifacts'],
            'resources_absent': True,
            'runner_absent': True,
        })
finally:
    run(['systemctl', '--user', 'start', 'cleanup-receipt-watchdog.service'], timeout=60)

timings['status'] = run(['python3', 'scripts/release-status.py'], timeout=120, log=output / f'rehearsal-{args.number}-status.log')
status = json.loads((ROOT / '.build/release-status.json').read_text())
backup = json.loads((ROOT / '.build/release/backup/metadata-manifest.json').read_text())
restore = json.loads((ROOT / '.build/release/backup/restore-verification.json').read_text())
report = {
    'kind': 'local-release-rehearsal/v1',
    'number': args.number,
    'started_at': started_at,
    'completed_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
    'source_revision': subprocess.run(['git', 'rev-parse', 'HEAD'], cwd=ROOT, capture_output=True, text=True, check=True).stdout.strip(),
    'stopped_before_start': True,
    'runtime_downloads': 0,
    'paid_services_used': False,
    'backup_sha256': backup['backup_sha256'],
    'backup_restore_matched': restore['matched'],
    'results': results,
    'service_status': status,
    'timings_seconds': timings,
}
target.write_text(json.dumps(report, indent=2) + '\n')
print(json.dumps(report, indent=2))
