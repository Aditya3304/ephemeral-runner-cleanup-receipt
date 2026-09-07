"""Probe live Sigstore endpoints from an actual runner after the existing gate."""
import datetime
import ipaddress
import json
import os
from pathlib import Path
import subprocess
import sys

repo = Path(__file__).resolve().parent.parent
os.chdir(repo)
network = "cleanup-receipt-sigstore_sigstore"
dc = ["docker", "compose", "-f", "compose.sigstore.yaml"]

def read(args):
    return subprocess.check_output(args, text=True, timeout=60)

net = json.loads(read(["docker", "network", "inspect", network]))[0]
assert net["Internal"] is True
targets = []
services = {}
for service, ports in {"issuer": [8443], "fulcio": [5555, 8081],
                       "ctlog": [6962], "rekor": [3000, 3001], "tsa": [3000]}.items():
    identifier = read(dc + ["ps", "-q", service]).strip()
    assert identifier, f"{service} is not running"
    container = json.loads(read(["docker", "inspect", identifier]))[0]
    assert container["State"]["Running"]
    assert not container["HostConfig"].get("PortBindings"), f"{service} publishes host ports"
    assert set(container["NetworkSettings"]["Networks"]) == {network}
    address = container["NetworkSettings"]["Networks"][network]["IPAddress"]
    assert ipaddress.ip_address(address).version == 4
    services[service] = {"container_id": identifier, "ip": address, "published_ports": []}
    targets.extend([[service, address, port] for port in ports])
node = json.loads(read(["docker", "inspect", "cleanup-receipt-control-plane"]))[0]
assert network not in node["NetworkSettings"]["Networks"]
# Positive controls: services are listening from an authorized network client.
subprocess.run(dc + ["run", "--rm", "--no-deps", "client", "/scripts/sigstore-check.sh", "ready"],
               check=True, timeout=180)
tcp_controls = "\n".join(f"timeout 3 bash -c 'exec 3<>/dev/tcp/{address}/{port}'"
                         for _, address, port in targets)
subprocess.run(dc + ["run", "--rm", "--no-deps", "client", "-ec", tcp_controls],
               check=True, timeout=45)
if sys.argv[1:] == ["--controls-only"]:
    print("All seven private TCP listeners reachable from authorized client; no host ports published.")
    raise SystemExit(0)
source = r'''
const net=require('node:net'), fs=require('node:fs'), https=require('node:https'), assert=require('node:assert/strict');
const targets=JSON.parse(process.argv[1]);
function connect(host,port){return new Promise(resolve=>{const s=net.connect({host,port,timeout:1500});s.once('connect',()=>{s.destroy();resolve(true)});s.once('timeout',()=>{s.destroy();resolve(false)});s.once('error',()=>resolve(false))})}
(async()=>{
 const base='/var/run/secrets/kubernetes.io/serviceaccount/';
 const status=await new Promise((ok,no)=>{const q=https.get({host:process.env.KUBERNETES_SERVICE_HOST,port:443,path:'/api/v1/namespaces/'+process.env.PROOF_NAMESPACE+'/configmaps',ca:fs.readFileSync(base+'ca.crt'),headers:{Authorization:'Bearer '+fs.readFileSync(base+'token','utf8')},timeout:5000},r=>{r.resume();r.on('end',()=>ok(r.statusCode))});q.on('error',no);q.on('timeout',()=>q.destroy(Error('API timeout')))});
 assert.equal(status,200,'authorized Kubernetes API control must work');
 for(const [name,ip,port] of targets){assert.equal(await connect(ip,port),false,name+' signing endpoint reachable');console.log('BLOCKED '+name+' '+ip+':'+port)}
 for(const path of ['/run/finalizer/client.key','/secrets/signing/client.key','/run/issuer/signing.key','/var/run/docker.sock'])assert.equal(fs.existsSync(path),false,path+' exposed');
 console.log('SIGSTORE_RUNNER_ISOLATION_PASSED');
})().catch(e=>{console.error(e);process.exit(1)});
'''
revision = read(["git", "rev-parse", "HEAD"]).strip()
result = subprocess.run([str(repo / ".build/localci"), "run", "--revision", revision,
                         "--job", "sigstore-isolation", "--timeout", "30s", "--",
                         "/usr/local/bin/node", "-e", source, json.dumps(targets)],
                        text=True, capture_output=True, timeout=240)
print(result.stdout, end="")
if result.returncode:
    raise RuntimeError(result.stderr)
summary = json.loads(result.stdout[result.stdout.index("{"):])
# Resolve only the returned run, not whichever concurrent ledger is newest.
run_id = summary.get("run_id") or summary.get("identity", {}).get("run_id")
if not run_id:
    raise RuntimeError("coordinator summary missing run identity")
directory = repo / ".build/coordinator" / run_id
ledger = json.loads((directory / "run.json").read_text())
guard = json.loads((directory / "guard.json").read_text())
assert ledger["phase"] == "complete" and ledger["runner_absent"] and ledger["resource_absent"]
assert guard["command_exit_code"] == 0 and guard["cleanup_exit_code"] == 0
assert "SIGSTORE_RUNNER_ISOLATION_PASSED" in (directory / "job.log").read_text()
report = {"kind": "sigstore-service-isolation/v1", "checked_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
          "network": network, "internal": True, "services": services, "targets": targets,
          "run_id": run_id, "gate": "existing coordinator/CRI-verified pod network gate",
          "authorized_api_control": "passed", "service_readiness_control": "passed",
          "private_tcp_controls": "all seven listeners reachable from authorized client",
          "signing_endpoint_connections": "all blocked", "host_published_ports": "none",
          "runner_cleanup": "passed"}
output = repo / ".build/sigstore/service-isolation.json"
output.write_text(json.dumps(report, indent=2) + "\n")
print("Live signing-service isolation passed; report:", output)
