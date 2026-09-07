#!/usr/bin/env python3
"""Create operator configuration from public trust exported out of trusted volumes."""
import hashlib
import json
import pathlib
import re

root = pathlib.Path(__file__).resolve().parent.parent
build = root / '.build'
revision = (build / 'finalizer-revision').read_text().strip()
if not re.fullmatch(r'[0-9a-f]{40}', revision):
    raise SystemExit('Invalid built finalizer revision')
def digest(name):
    return hashlib.sha256((build / name).read_bytes()).hexdigest()
config = {
    'input_dir': '/input', 'state_dir': '/state', 'output_dir': '/output',
    'archive_config': '/secrets/archive/config.json',
    'cosign_binary': '/usr/local/bin/cosign',
    'signing_config': '/trust/signing-config.json',
    'trust_root': '/trust/trusted-root.json',
    'tls_ca_file': '/trust/issuer-ca.crt',
    'policy': {
        'revision': revision, 'issuer': 'https://issuer:8443',
        'identity': 'finalizer@cleanup-receipt.local',
        'trust_root_sha256': digest('finalizer-trust/trusted-root.json'),
        'signing_config_sha256': digest('finalizer-trust/signing-config.json'),
        'cosign_sha256': digest('finalizer-image/cosign'),
    },
    'token': {
        'endpoint': 'https://issuer:8443/token',
        'ca_file': '/secrets/signing/ca.crt',
        'cert_file': '/secrets/signing/client.crt',
        'key_file': '/secrets/signing/client.key',
    },
}
(build / 'finalizer-config.json').write_text(json.dumps(config, indent=2) + '\n')
print('Prepared operator configuration from the installed local trust root.')
