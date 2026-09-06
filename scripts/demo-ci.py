"""Real local CI acceptance checks. Only project-owned test resources are touched."""
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid

repo = Path(__file__).resolve().parent.parent
os.chdir(repo)
cli = [str(repo / '.build/localci')]
kube = ['kubectl', '--kubeconfig', str(repo / '.build/kubeconfig'), '--context', 'kind-cleanup-receipt']
root = repo / '.build/coordinator'
revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
results = []

def call(args, **kwargs):
    return subprocess.run(args, capture_output=True, text=True, timeout=180, **kwargs)

def read_run(path):
    try:
        return json.loads(path.read_text())
    except (FileNotFoundError, json.JSONDecodeError):
        return None

def await_condition(fn, seconds=120):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        value = fn()
        if value:
            return value
        time.sleep(.3)
    raise RuntimeError('Timed out waiting for local job progress')

def stage_ready(ledger):
    l = read_run(ledger)
    if not l or not l.get('pod_uid'):
        return False
    r = call(kube + ['-n', l['runner_namespace'], 'get', 'configmap', 'guard-stage', '-o', 'json'])
    if r.returncode:
        return False
    stage = json.loads(r.stdout).get('data', {})
    status = json.loads(stage.get('guard.json', '{}'))
    return l if status.get('main_completed') else False

def run_case(case, mode, injection=None, extra=None):
    original_job_uid = None
    before = set(root.glob('*/run.json'))
    args = cli + ['run', '--revision', revision, '--job', case, '--timeout', '45s', '--',
                  '/usr/local/bin/node', '/opt/examples/local-job.mjs', mode] + (extra or [])
    process = subprocess.Popen(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    def discover():
        paths = set(root.glob('*/run.json')) - before
        if paths:
            return next(iter(paths))
        if process.poll() is not None:
            out, err = process.communicate()
            raise RuntimeError(f'{case} never started: {out}\n{err}')
        return False
    ledger = await_condition(discover)
    if injection:
        l = await_condition(lambda: stage_ready(ledger))
        original_job_uid = l['job_uid']
        if injection == 'cancel':
            r = call(cli + ['cancel', l['identity']['run_id']]); assert r.returncode == 0, r.stderr
        elif injection == 'admission':
            invalid = {'apiVersion':'v1','kind':'Pod','metadata':{'name':'admission-probe','namespace':l['resource_namespace']},
                       'spec':{'hostPID':True,'containers':[{'name':'probe','image':l['image'],'securityContext':{'privileged':True}}]}}
            r = call(kube + ['create','--dry-run=server','-f','-'],input=json.dumps(invalid))
            assert r.returncode != 0 and 'violates PodSecurity' in r.stderr, r.stderr
        elif injection == 'restart':
            process.kill()  # This harness owns this coordinator subprocess.
            process.communicate(timeout=15)
            process = subprocess.Popen(cli + ['collect', l['identity']['run_id']], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        elif injection == 'kill-runner':
            # Verify the exact pod UID before targeting its runtime container.
            r = call(kube + ['-n', l['runner_namespace'], 'get', 'pod', l['pod_name'], '-o', 'json']); assert r.returncode == 0, r.stderr
            pod = json.loads(r.stdout); assert pod['metadata']['uid'] == l['pod_uid']
            container = next(s for s in pod['status']['containerStatuses'] if s['name'] == 'guard')
            identifier = container['containerID'].removeprefix('containerd://')
            assert len(identifier) == 64 and all(c in '0123456789abcdef' for c in identifier)
            r = call(['docker', 'exec', 'cleanup-receipt-control-plane', 'ctr', '--namespace', 'k8s.io', 'tasks', 'kill', '--signal', 'SIGKILL', identifier])
            assert r.returncode == 0, r.stderr
    out, err = process.communicate(timeout=240)
    l = read_run(ledger)
    assert l['phase'] == 'complete', (case, out, err, l)
    assert l['runner_absent'] and l['resource_absent'], (case, l)
    status = read_run(ledger.parent / 'guard.json') or {}
    if injection == 'kill-runner':
        assert l['evidence_status'] == 'missing', l
        assert not status.get('post_completed'), status
        assert l['pod_exit_code'] != 0, l
    else:
        assert l['evidence_status'] == 'accepted-untrusted', (case, out, err, l)
        assert status['main_completed'] and status['post_completed'] and status['cleanup_exit_code'] == 0, status
        evidence = read_run(ledger.parent / 'cleanup-evidence.json')
        assert evidence['signature_state'] == 'unsigned' and evidence['verdict'] == 'partial', evidence
        for name in ['workspace', 'credentials', 'resources']:
            assert evidence['coverage'][name]['status'] == 'verified', evidence
        if mode == 'fail':
            assert status['command_exit_code'] == 7, status
        elif injection != 'cancel':
            assert status['command_exit_code'] == 0, status
        if injection == 'cancel':
            assert l['cancel_requested'] and status['cancellation_signal'] == 'SIGTERM', (l, status)
            assert not status['command_timed_out'], 'Cancellation was not delivered before the job timeout'
        if mode == 'isolation':
            assert 'ISOLATION_CHECKS_PASSED' in (ledger.parent / 'job.log').read_text()
        if injection == 'restart':
            assert l['job_uid'] == original_job_uid
            assert (ledger.parent / 'job.log').read_text().count('LOCAL_JOB_STARTED pass') == 1
    result = {'case': case, 'run_id': l['identity']['run_id'], 'pod_exit_code': l['pod_exit_code'],
              'evidence': l['evidence_status'], 'cleanup_exit_code': status.get('cleanup_exit_code'),
              'post_completed': status.get('post_completed'), 'resources_absent': l['resource_absent'],
              'runner_absent': l['runner_absent'], 'errors': l['errors']}
    results.append(result)
    print(json.dumps(result), flush=True)

selected = sys.argv[1:] or ['pass', 'fail', 'cancel', 'missing-post', 'restart', 'isolation']
for case in selected:
    print(f'Running real local case: {case}', flush=True)
    if case == 'pass': run_case(case, 'pass')
    elif case == 'fail': run_case(case, 'fail')
    elif case == 'cancel': run_case(case, 'wait', 'cancel')
    elif case == 'missing-post': run_case(case, 'wait', 'kill-runner')
    elif case == 'restart': run_case(case, 'pass', 'restart')
    elif case == 'isolation':
        token = uuid.uuid4().hex
        ns = 'proof-canary-' + token
        labels = {'cleanup-receipt.local/test': token}
        image = (repo / '.build/runner-image').read_text().strip()
        manifest = {'apiVersion': 'v1', 'kind': 'List', 'items': [
            {'apiVersion': 'v1', 'kind': 'Namespace', 'metadata': {'name': ns, 'labels': labels}},
            {'apiVersion': 'v1', 'kind': 'Pod', 'metadata': {'name': 'canary', 'namespace': ns, 'labels': labels},
             'spec': {'automountServiceAccountToken': False, 'restartPolicy': 'Never', 'terminationGracePeriodSeconds': 1,
                      'containers': [{'name': 'server', 'image': image, 'imagePullPolicy': 'Never',
                                      'command': ['/usr/local/bin/node', '-e', 'require("node:net").createServer(s=>{s.on("error",()=>{});s.end("canary")}).listen(8080,"0.0.0.0")'],
                                      'readinessProbe': {'tcpSocket': {'port': 8080}, 'periodSeconds': 1},
                                      'resources': {'limits': {'memory': '192Mi', 'cpu': '200m'}}}]}}
        ]}
        manifest['items'].append({'apiVersion':'v1','kind':'Pod','metadata':{'name':'host-canary','namespace':ns,'labels':labels},
            'spec':{'automountServiceAccountToken':False,'restartPolicy':'Never','hostNetwork':True,'terminationGracePeriodSeconds':1,
                    'containers':[{'name':'server','image':image,'imagePullPolicy':'Never',
                        'command':['/usr/local/bin/node','-e','for(const p of [18080,31080]){require("node:net").createServer(s=>{s.on("error",()=>{});s.on("data",b=>s.write(b))}).listen(p,"0.0.0.0");const u=require("node:dgram").createSocket("udp4");u.on("message",(b,r)=>u.send(b,r.port,r.address));u.bind(p,"0.0.0.0")}'],
                        'readinessProbe':{'tcpSocket':{'port':31080},'periodSeconds':1},
                        'resources':{'limits':{'memory':'192Mi','cpu':'200m'}}}]}})
        r = call(kube + ['create', '-f', '-'], input=json.dumps(manifest)); assert r.returncode == 0, r.stderr
        ns_data = json.loads(call(kube + ['get', 'namespace', ns, '-o', 'json']).stdout)
        try:
            r = call(kube + ['-n', ns, 'wait', '--for=condition=Ready', 'pod/canary', '--timeout=60s']); assert r.returncode == 0, r.stderr
            r = call(kube + ['-n', ns, 'wait', '--for=condition=Ready', 'pod/host-canary', '--timeout=60s']); assert r.returncode == 0, r.stderr
            pod = json.loads(call(kube + ['-n', ns, 'get', 'pod', 'canary', '-o', 'json']).stdout)
            ip = pod['status']['podIP']
            nodeip = pod['status']['hostIP']
            # The unrestricted canary confirms the target is listening before
            # the actual guarded job is required to fail to connect to it.
            r = call(kube + ['-n', ns, 'exec', 'canary', '--', '/usr/local/bin/node', '-e',
                            f'const s=require("node:net").connect(8080,{json.dumps(ip)},()=>{{s.destroy();process.exit(0)}});s.setTimeout(2000,()=>process.exit(1));s.on("error",()=>process.exit(1))'])
            assert r.returncode == 0, r.stderr
            # Both TCP listeners and nonce-based UDP echoes must be reachable
            # from an unrestricted ordinary pod before guarded denial counts.
            control = ('const host=' + json.dumps(nodeip) + ';'
                'for(const p of [18080,31080]){await new Promise((ok,no)=>{const s=require("node:net").connect(p,host,()=>{s.destroy();ok()});s.setTimeout(2000,()=>{s.destroy();no(Error("tcp timeout"))});s.on("error",no)});'
                'await new Promise((ok,no)=>{const u=require("node:dgram").createSocket("udp4"),n=Buffer.from("control-nonce"),t=setTimeout(()=>{u.close();no(Error("udp timeout"))},2000);u.on("error",no);u.on("message",b=>{clearTimeout(t);u.close();b.equals(n)?ok():no(Error("nonce"))});u.send(n,p,host)})}')
            r = call(kube + ['-n', ns, 'exec', 'canary', '--', '/usr/local/bin/node', '-e', '(async()=>{' + control + '})().catch(e=>{console.error(e);process.exit(1)})'])
            assert r.returncode == 0, r.stderr
            run_case(case, 'isolation', 'admission', extra=[ip, nodeip])
        finally:
            current = call(kube + ['get', 'namespace', ns, '-o', 'json'])
            if current.returncode == 0:
                obj = json.loads(current.stdout)
                assert obj['metadata']['uid'] == ns_data['metadata']['uid'] and obj['metadata']['labels']['cleanup-receipt.local/test'] == token
                r = call(kube + ['delete', 'namespace', ns, '--wait=true', '--timeout=60s']); assert r.returncode == 0, r.stderr
    else: raise ValueError(case)
target = repo / '.build' / f'ci-validation-{time.time_ns()}.json'
target.write_text(json.dumps({'kind': 'local-ci-validation/v1', 'results': results}, indent=2))
print(f'Results saved: {target}')
