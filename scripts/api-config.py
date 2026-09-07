#!/usr/bin/env python3
"""Pin API policy from the operator's installed finalizer config, never a submission."""
import json
from pathlib import Path
root = Path(__file__).resolve().parent.parent
finalizer = json.loads((root / '.build/finalizer-config.json').read_text())
config = {
    'repository': 'Aditya3304/ephemeral-runner-cleanup-receipt',
    'archive_config': '/run/archive-public/verifier.json',
    'cosign_binary': '/usr/local/bin/cosign',
    'signing_config': '/trust/signing-config.json',
    'trust_root': '/trust/trusted-root.json',
    'token_file': '/run/auth/token',
    'policy': finalizer['policy'],
}
target = root / '.build/api-config.json'
if target.exists() and json.loads(target.read_text()) != config:
    raise SystemExit('API policy differs from installed finalizer. Review and explicitly update .build/api-config.json; existing historical policy is preserved.')
target.write_text(json.dumps(config, indent=2) + '\n')
print('API pinned to the locally approved finalizer policy.')
