"""Shared bounded helpers for milestone h's local fault demonstrations."""
import datetime, hashlib, json, os, pathlib, subprocess, time, urllib.error, urllib.request

ROOT = pathlib.Path(__file__).resolve().parent.parent
API_IMAGE = (ROOT / '.build/api-image-id').read_text().strip()
ENV = dict(os.environ, API_IMAGE=API_IMAGE)

def command(args, check=True, timeout=300, input=None):
    result = subprocess.run(args, cwd=ROOT, env=ENV, capture_output=True, text=True, timeout=timeout, input=input)
    if check and result.returncode:
        raise RuntimeError(f"Local command failed: {' '.join(map(str,args[:5]))}\n{result.stdout[-1500:]}\n{result.stderr[-1500:]}")
    return result

def get(path, timeout=50):
    try:
        with urllib.request.urlopen('http://127.0.0.1:8080' + path, timeout=timeout) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)

def wait_api():
    for _ in range(60):
        try:
            if get('/readyz', 3)[0] == 200:
                return
        except OSError:
            pass
        time.sleep(1)
    raise RuntimeError('Local API did not become ready')

def new_run(case='pass'):
    result = command(['python3', 'scripts/demo-ci.py', case], timeout=420)
    rows = []
    for line in result.stdout.splitlines():
        try:
            value = json.loads(line)
            if value.get('case') == case and value.get('run_id'):
                rows.append(value)
        except (json.JSONDecodeError, AttributeError):
            pass
    if len(rows) != 1:
        raise RuntimeError('Expected exactly one new local run')
    return rows[0]

def state_key(identity):
    value = dict(identity, source_revision='')
    raw = json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False).encode()
    return hashlib.sha256(raw).hexdigest()

def watchdog_state(identity):
    path = ROOT / '.build/watchdog-state' / (state_key(identity) + '.json')
    return json.loads(path.read_text())

def watchdog_once(run_id):
    return command(['python3', 'scripts/watchdog-run.py', '--run', run_id], check=False, timeout=360)

def wait_due(value):
    due = datetime.datetime.fromisoformat(value.replace('Z', '+00:00')).timestamp()
    while time.time() <= due:
        time.sleep(min(3, max(.1, due - time.time() + .1)))

def receipt_for(run_id):
    code, page = get('/v1/receipts?run_id=' + run_id + '&limit=100')
    if code != 200 or len(page['items']) != 1:
        raise RuntimeError(f'Expected one API receipt for {run_id}')
    return page['items'][0]

def verify_receipt(receipt_id):
    code, verification = get('/v1/receipts/' + receipt_id + '/verification')
    if code != 200 or verification.get('signature') != 'verified' or verification.get('artifacts') != 'verified':
        raise RuntimeError('Independent API verification failed')
    return verification

def published_dir(run_id):
    matches=[]
    for result in (ROOT/'.build/finalized').glob('*/result.json'):
        value=json.loads(result.read_text())
        if value.get('identity',{}).get('run_id') == run_id:
            matches.append(result.parent)
    if len(matches) != 1:
        raise RuntimeError('Expected one locally published result')
    return matches[0]

def deliver(run_id, check=True):
    directory=published_dir(run_id)
    return command(['docker','compose','-f','compose.api.yaml','run','--rm','--no-deps','deliver','/usr/local/bin/deliver','/input/'+directory.name+'/result.json'],check=check,timeout=180)

def parse_delivery(result):
    rows=[]
    for line in result.stdout.splitlines():
        try:
            value=json.loads(line)
            if value.get('status'): rows.append(value)
        except (json.JSONDecodeError,AttributeError): pass
    if len(rows)!=1: raise RuntimeError('Expected one durable delivery result')
    return rows[0]

def utc_now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()
