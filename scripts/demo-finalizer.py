#!/usr/bin/env python3
"""Run real local jobs, archive/sign their receipts, and verify without a network."""
import json
import pathlib
import subprocess
import sys
import time

root = pathlib.Path(__file__).resolve().parent.parent
cases = sys.argv[1:] or ['pass', 'fail']
if any(case not in ('pass', 'fail', 'cancel', 'missing-post', 'restart', 'isolation') for case in cases):
    raise SystemExit('Unknown local lifecycle case')
process = subprocess.Popen(['python3', 'scripts/demo-ci.py', *cases], cwd=root, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
runs = []
for line in process.stdout:
    print(line, end='', flush=True)
    try:
        value = json.loads(line)
        if isinstance(value, dict) and value.get('case') in cases and value.get('run_id'):
            runs.append(value)
    except json.JSONDecodeError:
        pass
if process.wait() != 0 or len(runs) != len(cases):
    raise SystemExit('Local lifecycle did not produce all expected runs')
results = []
for run in runs:
    run_id = run['run_id']
    subprocess.run(['bash', 'scripts/finalizer-run.sh', run_id], cwd=root, check=True)
    subprocess.run(['python3', 'scripts/finalizer-verify.py', run_id], cwd=root, check=True)
    matches = []
    for path in (root / '.build/finalized').glob('*/receipt.json'):
        receipt = json.loads(path.read_bytes())
        if receipt['identity']['run_id'] == run_id:
            matches.append((path.parent, receipt))
    if len(matches) != 1:
        raise SystemExit('Ambiguous final receipt')
    directory, receipt = matches[0]
    # An ordinary command failure does not imply cleanup failed. The independent
    # collector must prove every required component before either can be a pass.
    expected = 'fail' if run['case'] == 'missing-post' else 'pass'
    if receipt['verdict'] != expected:
        raise SystemExit(f"Unexpected cleanup verdict for {run['case']}: {receipt['verdict']}; expected {expected}")
    result = json.loads((directory / 'result.json').read_bytes())
    results.append({'case': run['case'], 'run_id': run_id, 'command_exit': run['pod_exit_code'], 'verdict': receipt['verdict'], 'signature_state': result['signature_state'], 'receipt': result['receipt'], 'bundle': result['bundle'], 'coverage': receipt['coverage'], 'source_revision': receipt['identity']['source_revision'], 'finalizer_revision': receipt['finalizer']['revision']})
report = root / '.build' / f'finalizer-validation-{time.time_ns()}.json'
report.write_text(json.dumps({'kind': 'local-finalizer-validation/v1', 'results': results}, indent=2) + '\n')
print(f'Results saved: {report}')
