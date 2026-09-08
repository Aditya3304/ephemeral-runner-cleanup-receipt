#!/usr/bin/env python3
"""Read-only, credential-free view of the coordinator's actual durable ledger.

Run on the AWS host. It does not infer success from filenames or change state.
Missing fields are unknown, not false; intermediate phases can be shorter than polling.
"""
import argparse, datetime, json, pathlib, time

parser = argparse.ArgumentParser()
parser.add_argument('--run', help='Optional numeric GitHub run ID')
parser.add_argument('--once', action='store_true')
args = parser.parse_args()
if args.run and not args.run.isdecimal():
    raise SystemExit('Expected numeric GitHub run ID')
root = pathlib.Path(__file__).resolve().parents[2] / '.build/coordinator'
previous = None
try:
    while True:
        rows = []
        for path in root.glob('*/run.json'):
            try:
                row = json.loads(path.read_text())
                if not args.run or row.get('identity', {}).get('run_id') == args.run:
                    rows.append((row.get('created_at', ''), path, row))
            except (OSError, ValueError):
                continue
        if rows:
            _, path, row = max(rows, key=lambda item: item[0])
            display = {key: row.get(key) for key in [
                'identity', 'token', 'phase', 'runner_namespace', 'pod_name',
                'pod_uid', 'resource_namespace', 'collector_sha256', 'logs_status',
                'logs_bytes', 'resource_absent', 'runner_absent', 'role_absent',
                'binding_absent', 'github_assignment_confirmed', 'github_runner_absent',
            ]}
            display['published_files'] = [name for name in ['collector.json', 'observations.json', 'finalization-ready.json'] if (path.parent / name).is_file()]
        else:
            display = {'state': 'No matching ledger yet; waiting for a registered run.'}
        encoded = json.dumps(display, indent=2)
        if encoded != previous:
            print(datetime.datetime.now(datetime.timezone.utc).isoformat(), flush=True)
            print(encoded, flush=True)
            previous = encoded
        if args.once:
            break
        time.sleep(1)
except KeyboardInterrupt:
    pass
