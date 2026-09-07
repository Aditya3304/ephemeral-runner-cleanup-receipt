#!/usr/bin/env python3
"""Verify the prepared local release without contacting an external service."""
import datetime
import hashlib
import json
import pathlib
import subprocess
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parent.parent
CONTAINERS = {
    'kubernetes': 'cleanup-receipt-control-plane',
    'postgresql': 'cleanup-receipt-db-1',
    'kes': 'cleanup-receipt-archive-kes-1',
    'minio': 'cleanup-receipt-archive-minio-1',
    'issuer': 'cleanup-receipt-sigstore-issuer-1',
    'ctlog': 'cleanup-receipt-sigstore-ctlog-1',
    'fulcio': 'cleanup-receipt-sigstore-fulcio-1',
    'rekor': 'cleanup-receipt-sigstore-rekor-1',
    'tsa': 'cleanup-receipt-sigstore-tsa-1',
    'api': 'cleanup-receipt-api-api-1',
    'gateway': 'cleanup-receipt-api-gateway-1',
}

def command(args, timeout=60):
    return subprocess.run(args, cwd=ROOT, capture_output=True, text=True, timeout=timeout, check=True).stdout.strip()

def get(path):
    with urllib.request.urlopen('http://127.0.0.1:8080' + path, timeout=30) as response:
        return response.status, response.read()

running = {}
for name, container in CONTAINERS.items():
    value = command(['docker', 'container', 'inspect', '-f', '{{.State.Running}}', container])
    if value != 'true':
        raise SystemExit(f'{name} is not running')
    running[name] = container

if command(['kubectl', '--kubeconfig', str(ROOT / '.build/kubeconfig'), 'get', '--raw=/readyz']) != 'ok':
    raise SystemExit('Kubernetes is not ready')
if command(['systemctl', '--user', 'is-active', 'cleanup-receipt-watchdog.service']) != 'active':
    raise SystemExit('Watchdog is not active')
if command(['systemctl', '--user', 'is-enabled', 'cleanup-receipt-watchdog.service']) != 'enabled':
    raise SystemExit('Watchdog is not enabled')
if command(['loginctl', 'show-user', command(['id', '-un']), '-p', 'Linger', '--value']) != 'yes':
    raise SystemExit('WSL user lingering is not enabled')

status, ready = get('/readyz')
if status != 200 or json.loads(ready) != {'status': 'ready'}:
    raise SystemExit('API is not ready')
status, page = get('/')
if status != 200 or b'AFTER' not in page:
    raise SystemExit('Dashboard is not available')
_, payload = get('/v1/receipts?limit=100')
receipts = json.loads(payload)['items']
if not receipts:
    raise SystemExit('No verified receipt is available')
latest = receipts[0]
_, payload = get('/v1/receipts/' + latest['id'] + '/verification')
verification = json.loads(payload)
if verification.get('signature') != 'verified' or verification.get('artifacts') != 'verified':
    raise SystemExit('Latest receipt did not independently verify')

runner = (ROOT / '.build/runner-image').read_text().strip()
node_images = command(['docker', 'exec', CONTAINERS['kubernetes'], 'ctr', '--namespace', 'k8s.io', 'images', 'list', '-q'])
if 'docker.io/' + runner not in node_images.splitlines():
    raise SystemExit('Pinned runner image is not preloaded')
api_image = (ROOT / '.build/api-image-id').read_text().strip()
if command(['docker', 'container', 'inspect', '-f', '{{.Image}}', CONTAINERS['api']]) != api_image:
    raise SystemExit('Running API image differs from the pinned build')
watchdog_current = json.loads((ROOT / '.build/watchdog-current.json').read_text())
watchdog_binary = pathlib.Path(watchdog_current['binary'])
watchdog_config = json.loads(pathlib.Path(watchdog_current['config']).read_text())
watchdog_sha = hashlib.sha256(watchdog_binary.read_bytes()).hexdigest()
if watchdog_sha != hashlib.sha256((ROOT / '.build/watchdog').read_bytes()).hexdigest():
    raise SystemExit('Installed watchdog binary differs from the pinned build')
if watchdog_config['api_image'] != api_image or watchdog_config['finalizer_image'] != (ROOT / '.build/finalizer-image-id').read_text().strip():
    raise SystemExit('Installed watchdog image policy differs from release assets')

report = {
    'kind': 'local-release-status/v1',
    'checked_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
    'version': (ROOT / 'VERSION').read_text().strip(),
    'containers': running,
    'kubernetes': 'ready',
    'watchdog': {'active': True, 'enabled': True, 'linger': True},
    'dashboard': 'http://localhost:8080/',
    'receipt_count': len(receipts),
    'latest_receipt': {
        'id': latest['id'],
        'verdict': latest['verdict'],
        'signature': verification['signature'],
        'artifacts': verification['artifacts'],
    },
    'images': {
        'runner': runner,
        'finalizer': (ROOT / '.build/finalizer-image-id').read_text().strip(),
        'api': api_image,
    },
    'watchdog_binary_sha256': watchdog_sha,
    'trust_root_sha256': hashlib.sha256((ROOT / '.build/finalizer-trust/trusted-root.json').read_bytes()).hexdigest(),
    'external_runtime_requests': 0,
    'paid_services_used': False,
}
target = ROOT / '.build/release-status.json'
target.write_text(json.dumps(report, indent=2) + '\n')
print(json.dumps(report, indent=2))
