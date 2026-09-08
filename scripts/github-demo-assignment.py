#!/usr/bin/env python3
"""Provision job-side demo evidence only; never produces trusted observations."""
import json
import os
import pathlib
import secrets
import subprocess

if os.environ.get('GITHUB_ACTIONS') != 'true':
    raise SystemExit('Only run inside the disposable GitHub demo job')
root = pathlib.Path(__file__).resolve().parent.parent
control = pathlib.Path(os.environ['RUNNER_TEMP']).resolve() / 'cleanup-control'
data = pathlib.Path(os.environ['RUNNER_TEMP']).resolve() / 'cleanup-data'
control.mkdir(mode=0o700)
data.mkdir(mode=0o700)
token = secrets.token_hex(16)
namespace = 'proof-' + token
kubeconfig = str(root / '.build/kubeconfig')
def kubectl(*args):
    return subprocess.check_output(['kubectl', '--kubeconfig', kubeconfig, *args], text=True)
kubectl('create', 'namespace', namespace)
kubectl('label', 'namespace', namespace, 'cleanup-receipt.local/run=' + token)
assignment = {
    'kind': 'cleanup-assignment/v1',
    'identity': {'provider': 'github', 'repository': os.environ['GITHUB_REPOSITORY'],
                 'run_id': os.environ['GITHUB_RUN_ID'], 'run_attempt': int(os.environ['GITHUB_RUN_ATTEMPT']),
                 'job_id': os.environ['GITHUB_JOB'], 'source_revision': os.environ['GITHUB_SHA']},
    'token': token, 'namespace': namespace,
    'namespace_uid': json.loads(kubectl('get', 'namespace', namespace, '-o', 'json'))['metadata']['uid'],
    'cluster_uid': json.loads(kubectl('get', 'namespace', 'kube-system', '-o', 'json'))['metadata']['uid'],
}
(control / 'assignment.json').write_text(json.dumps(assignment))
# This kubeconfig controls only the throwaway cluster inside this job. It is
# intentionally NOT a trusted collector credential or proof of VM isolation.
env = {'PROOF_CTL': str(root / '.build/proofctl'), 'PROOF_ASSIGNMENT': str(control / 'assignment.json'),
       'PROOF_KUBECONFIG': kubeconfig, 'PROOF_STATE': str(control / 'context.json'),
       'PROOF_SANDBOX_ROOT': str(data), 'PROOF_TRANSPORT': 'github',
       'PROOF_CLEANUP_TIMEOUT_MS': '30000'}
with open(os.environ['GITHUB_ENV'], 'a') as output:
    for key, value in env.items():
        if '\n' in value or '\r' in value:
            raise SystemExit('Unsafe environment path')
        output.write(key + '=' + value + '\n')
