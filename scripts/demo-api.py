#!/usr/bin/env python3
"""Ingest genuine local signed runs and exercise a recoverable database outage.

Only this project's database/API are briefly stopped. No data/volumes are deleted.
The outage runs once while a genuine fixture is still awaiting ingestion.
"""
import datetime
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.request

root = Path(__file__).resolve().parent.parent
os.chdir(root)
env = dict(os.environ, API_IMAGE=(root / '.build/api-image-id').read_text().strip())
compose = ['docker', 'compose', '-f', 'compose.api.yaml']

def command(args, check=True):
    p = subprocess.run(args, env=env, text=True, capture_output=True)
    if check and p.returncode:
        raise RuntimeError(f'Local command failed: {args[:4]}\n{p.stdout}\n{p.stderr}')
    return p

def get(path):
    try:
        with urllib.request.urlopen('http://127.0.0.1:8080'+path, timeout=50) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as e:
        return e.code, json.load(e)

def ready():
    for _ in range(45):
        try:
            if get('/readyz')[0] == 200:
                return
        except OSError:
            pass
        time.sleep(1)
    raise RuntimeError('Local API did not become ready')

def deliver(path, check=True):
    p = command(compose + ['run', '--rm', '--no-deps', 'deliver', '/usr/local/bin/deliver', '/input/'+path.parent.name+'/result.json'], check=check)
    lines = [json.loads(line) for line in p.stdout.splitlines() if line.startswith('{')]
    if len(lines) != 1:
        raise RuntimeError('Expected exactly one durable delivery result')
    return p.returncode, lines[0]

policy = json.loads((root / '.build/api-config.json').read_text())['policy']
fixtures = []
for p in (root / '.build/finalized').glob('*/result.json'):
    receipt = json.loads((p.parent / 'receipt.json').read_text())
    if receipt['finalizer'] == policy:
        fixtures.append((p, receipt))
fixtures.sort(key=lambda x: x[1]['identity']['job_id'])
assert len(fixtures) >= 6, 'Six genuine signed fixtures required'
ready()
report = {'checked_at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'api_image': env['API_IMAGE'], 'runs': [], 'database_outage': None}
existing = get('/v1/receipts?limit=100')[1]['items']
existing_runs = {r['run_id'] for r in existing}
pending = [(p, r) for p, r in fixtures if r['identity']['run_id'] not in existing_runs]
outage_target = pending[-1][0] if pending else None

for path, receipt in fixtures:
    if path == outage_target:
        print('Checking delivery while PostgreSQL is stopped; signed artifacts remain archived.', flush=True)
        command(['docker', 'compose', 'stop', 'db'])
        try:
            assert get('/readyz')[0] == 503
            rc, failed = deliver(path, check=False)
            assert rc != 0 and failed['status'] == 'retry' and failed['next_retry_at']
            command(compose + ['restart', 'api'])
        finally:
            command(['docker', 'compose', 'up', '-d', '--wait', 'db'])
        ready()
        due = datetime.datetime.fromisoformat(failed['next_retry_at'].replace('Z', '+00:00')).timestamp()
        while time.time() <= due:
            time.sleep(min(3, due - time.time() + 0.1))
        rc, recovered = deliver(path)
        assert recovered['status'] == 'complete' and recovered['attempts'] == 2
        report['database_outage'] = {'run_id': receipt['identity']['run_id'], 'failure': failed, 'recovery': recovered, 'api_restarted_between_attempts': True}
    rc, delivered = deliver(path)
    assert delivered['status'] == 'complete'
    code, verification = get('/v1/receipts/'+delivered['receipt_id']+'/verification')
    assert code == 200 and verification['signature'] == verification['artifacts'] == 'verified'
    code, row = get('/v1/receipts/'+delivered['receipt_id'])
    assert code == 200 and row['verdict'] == receipt['verdict']
    assert (row['incident'] is None) == (row['verdict'] == 'pass')
    report['runs'].append({'run_id': row['run_id'], 'job': row['job_id'], 'verdict': row['verdict'], 'receipt_id': row['id'], 'incident_id': row['incident']['id'] if row['incident'] else None, 'verification': verification})
    print(f"{row['job_id']}: {row['verdict']}, signature and archived artifacts verified", flush=True)

code, failures = get('/v1/receipts?verdict=fail&incident_state=open')
assert code == 200 and failures['items']
report['open_incidents'] = len(get('/v1/incidents')[1]['items'])
target = root / '.build/api-demo-results.json'
if report['database_outage'] is None and target.exists():
    report['database_outage'] = json.loads(target.read_text()).get('database_outage')
target.write_text(json.dumps(report, indent=2)+'\n')
print('API demo complete. Open http://localhost:8080/v1/receipts for stored metadata.')
