"""A real, repeatable CLI demonstration; no finalizer or signed receipt is simulated."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time

repo = Path(__file__).resolve().parent.parent
os.chdir(repo)
run = repo / ".build" / "demo" / str(time.time_ns())
run.mkdir(parents=True)
state = run / "context.json"
kubeconfig = repo / ".build" / "kubeconfig"
proof = [str(repo / ".build" / "proofctl"), "--state", str(state), "--kubeconfig", str(kubeconfig), "--context", "kind-cleanup-receipt"]
kube = ["kubectl", "--kubeconfig", str(kubeconfig), "--context", "kind-cleanup-receipt"]

def call(args):
    result = subprocess.run(args, text=True, capture_output=True, timeout=150)
    if result.returncode:
        raise RuntimeError(f"Command failed: {args[0]}\n{result.stderr}")
    return result.stdout

print("Creating an owned namespace and disposable workspace...", flush=True)
sandbox = tempfile.mkdtemp(prefix="proofctl-demo-")
revision = call(["git", "rev-parse", "HEAD"]).strip()
context = json.loads(call(proof + ["begin", "--sandbox-root", sandbox, "--repository", "Aditya3304/ephemeral-runner-cleanup-receipt", "--run", run.name, "--job", "cleanup-demo", "--revision", revision]))
namespace = context["namespace"]
print(f"Namespace: {namespace}", flush=True)
call(kube + ["-n", namespace, "create", "serviceaccount", "demo-job"])
call(kube + ["-n", namespace, "create", "configmap", "demo-output", "--from-literal=result=example"])
call(kube + ["-n", namespace, "create", "secret", "generic", "demo-credential", "--from-literal=example=disposable-test-value"])
workspace = Path(context["sandbox"]) / "workspace"
credentials = Path(context["sandbox"]) / "credentials"
(workspace / ".hidden").write_text("disposable workspace content")
(credentials / "example-token").write_text("disposable local test credential")
sentinel = Path(sandbox) / "outside-workspace-sentinel"
sentinel.write_text("preserve this file")
(workspace / "symlink-outside").symlink_to(sentinel)
inventory = json.loads(call(proof + ["inventory"]))
print(f"Inventoried {len(inventory['resources'])} resources. Running cleanup...", flush=True)
result = json.loads(call(proof + ["cleanup", "--timeout", "120s"]))
assert not list(workspace.iterdir()), "workspace not empty"
assert not list(credentials.iterdir()), "credential directory not empty"
assert sentinel.read_text() == "preserve this file", "symlink escaped the workspace"
assert not call(kube + ["get", "namespace", namespace, "--ignore-not-found", "-o", "name"]).strip()
call(proof + ["cleanup", "--timeout", "120s"])
evidence_file = run / "cleanup-evidence.json"
call(proof + ["receipt", "--out", str(evidence_file)])
raw = evidence_file.read_bytes()
evidence = json.loads(raw)
assert evidence["verdict"] == "partial" and evidence["signature_state"] == "unsigned"
print("Confirmed: namespace/resources absent, workspace empty, credential files removed.")
print("Confirmed: outside sentinel survived; repeated cleanup succeeded.")
print("Preliminary evidence: PARTIAL / UNSIGNED — logs and runner disposal are not observed yet.")
print(f"Evidence SHA-256: {hashlib.sha256(raw).hexdigest()}")
print(f"Evidence saved to: {evidence_file}")
print("Owned empty sandbox retained for inspection. No metadata was inserted into PostgreSQL.")
fixture = repo / "internal" / "proof" / "testdata" / "sigstore"
call(proof + ["verify", "--artifact", str(fixture / "artifact.txt"),
              "--bundle", str(fixture / "bundle.json"), "--trust-root", str(fixture / "trusted-root.json"),
              "--sha256", hashlib.sha256((fixture / "artifact.txt").read_bytes()).hexdigest(),
              "--identity", "https://github.com/sigstore-conformance/extremely-dangerous-public-oidc-beacon/.github/workflows/extremely-dangerous-oidc-beacon.yml@refs/heads/main",
              "--issuer", "https://token.actions.githubusercontent.com"])
print("Separately verified a real upstream Sigstore test bundle offline. The cleanup evidence remains unsigned.")
