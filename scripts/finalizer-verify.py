#!/usr/bin/env python3
"""Verify a published receipt with proofctl in a fresh network-disabled container."""
import hashlib
import json
import pathlib
import re
import subprocess
import sys

root = pathlib.Path(__file__).resolve().parent.parent
run = sys.argv[1] if len(sys.argv) == 2 else ''
if not re.fullmatch(r'[0-9a-f]{32}', run):
    raise SystemExit('Usage: python3 scripts/finalizer-verify.py <run-id>')
image = (root / '.build/finalizer-image-id').read_text().strip()
if not re.fullmatch(r'sha256:[0-9a-f]{64}', image):
    raise SystemExit('Invalid local finalizer image identity')
matches = []
for result in (root / '.build/finalized').glob('*/result.json'):
    data = json.loads(result.read_bytes())
    if data.get('identity', {}).get('run_id') == run:
        matches.append((result.parent, data))
if len(matches) != 1:
    raise SystemExit('Expected exactly one published result for this run')
directory, result = matches[0]
receipt = (directory / 'receipt.json').read_bytes()
bundle = (directory / 'receipt.bundle.json').read_bytes()
for name, content in [('receipt', receipt), ('bundle', bundle)]:
    if hashlib.sha256(content).hexdigest() != result[name]['sha256'] or len(content) != result[name]['size_bytes']:
        raise SystemExit('Published bytes differ from immutable archive reference')
trust = root / '.build/finalizer-trust'
args = ['docker', 'run', '--rm', '--network', 'none', '--read-only',
        '--user', '65532:65532', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
        '--memory', '512m', '--tmpfs', '/tmp:rw,noexec,nosuid,size=64m,uid=65532,gid=65532,mode=700',
        '-v', f'{directory}:/artifacts:ro', '-v', f'{trust}:/trust:ro',
        '-v', f'{root / "internal/proof/testdata/sigstore/trusted-root.json"}:/other-root.json:ro',
        '--entrypoint', '/usr/local/bin/proofctl', image, 'verify',
        '--cosign', '/usr/local/bin/cosign', '--bundle', '/artifacts/receipt.bundle.json']

def verify(case, artifact, sha, identity='finalizer@cleanup-receipt.local', issuer='https://issuer:8443', root_file='/trust/trusted-root.json', succeeds=False):
    command = args + ['--artifact', artifact, '--sha256', sha, '--identity', identity, '--issuer', issuer, '--trust-root', root_file]
    process = subprocess.run(command, capture_output=True, text=True, timeout=60)
    passed = (process.returncode == 0) == succeeds
    if not passed:
        raise SystemExit(f'Verification check failed: {case}: {process.stderr[-1500:]}')
    print(f'{case}: passed', flush=True)
    return {'case': case, 'passed': True, 'exit_code': process.returncode, 'network': 'none'}

checks = [verify('real signed receipt', '/artifacts/receipt.json', result['receipt']['sha256'], succeeds=True)]
# Recompute the digest of altered bytes so rejection tests the actual signature,
# rather than stopping at the preliminary expected-digest check.
altered = receipt + b'\n'
(directory / 'tampered-receipt.json').write_bytes(altered)
checks.append(verify('altered bytes with matching supplied digest', '/artifacts/tampered-receipt.json', hashlib.sha256(altered).hexdigest()))
checks.append(verify('wrong signer', '/artifacts/receipt.json', result['receipt']['sha256'], identity='runner@cleanup-receipt.local'))
checks.append(verify('wrong issuer', '/artifacts/receipt.json', result['receipt']['sha256'], issuer='https://wrong-issuer.invalid'))
checks.append(verify('unrelated trust root', '/artifacts/receipt.json', result['receipt']['sha256'], root_file='/other-root.json'))
document = json.loads(receipt)
references = dict(document['objects'])
references.update({f'job-{index}.log': ref for index, ref in enumerate(document['log_objects'])})
references.update({'receipt.json': result['receipt'], 'receipt.bundle.json': result['bundle']})
archived = []
for name, ref in references.items():
    # This container receives read-only verifier credentials. It cannot upload,
    # renew retention, obtain signing tokens, or reach the signing network.
    path = directory / 'archive-reference.json'
    path.write_text(json.dumps(ref) + '\n')
    command = ['docker', 'run', '--rm', '--network', 'cleanup-receipt-archive', '--read-only',
               '--user', '65532:65532', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges', '--memory', '512m',
               '-v', f'{path}:/reference.json:ro',
               '-v', 'cleanup-receipt-archive_public:/run/archive-public:ro',
               '-v', 'cleanup-receipt-archive_verifier:/run/archive-verifier:ro',
               '--entrypoint', '/usr/local/bin/archivectl', image, 'read',
               '--config', '/run/archive-public/verifier.json', '--reference', '/reference.json']
    fetched = subprocess.run(command, capture_output=True, timeout=150)
    if fetched.returncode != 0:
        raise SystemExit(f'Independent archive read failed for {name}: {fetched.stderr[-1500:].decode(errors="replace")}')
    if len(fetched.stdout) != ref['size_bytes'] or hashlib.sha256(fetched.stdout).hexdigest() != ref['sha256']:
        raise SystemExit(f'Independent archive content mismatch: {name}')
    archived.append({'name': name, 'version_id': ref['version_id'], 'sha256': ref['sha256'], 'size_bytes': ref['size_bytes'], 'passed': True})
    print(f'Independent exact-version archive read: {name}: passed', flush=True)
report = {'kind': 'local-finalizer-verification/v1', 'run_id': run, 'receipt_sha256': result['receipt']['sha256'], 'checks': checks, 'archive_checks': archived}
(directory / 'verification.json').write_text(json.dumps(report, indent=2) + '\n')
print(f'Verification record saved: {directory / "verification.json"}')
