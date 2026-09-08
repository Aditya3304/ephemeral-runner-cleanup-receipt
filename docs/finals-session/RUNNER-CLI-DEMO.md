# Demonstrate the actual runner CLI

`gh` dispatches GitHub workflows. `kubectl` inspects Kubernetes. `proofctl` is
the Go CLI invoked by the TypeScript guard inside the runner.

Use the original prepared laptop session, with AWS services, bridge and tunnels
running. These commands refer to that environment, not a portable fresh checkout.

In an Ubuntu/WSL terminal:

```bash
cd /mnt/c/Users/Aditya/Documents/Codex/2026-09-08/i-have-a-hackathon-finals-round
export PATH=/tmp/cleanup-ssm/usr/local/sessionmanagerplugin/bin:$PATH
bash work/aws.sh ssm start-session --target i-0f21b25d3a04d8541
```

Inside the AWS shell:

```bash
sudo -i
cd /opt/cleanup/repo
export KUBECONFIG=/opt/cleanup/repo/.build/kubeconfig
```

In another laptop terminal, from the original session directory:

```bash
python3 -u work/run-walkthrough.py normal
```

During the 45-second observation pause, find the current run's pod in the AWS
shell. Match it with the ledger if multiple pods exist.

```bash
kubectl get pods -A
RUNNER_NS='PASTE_RUNNER_NAMESPACE'
POD='PASTE_POD_NAME'
kubectl exec -n "$RUNNER_NS" "$POD" -c guard -- /usr/local/bin/proofctl --help
kubectl exec -n "$RUNNER_NS" "$POD" -c guard -- /usr/local/bin/proofctl cleanup --help
```

Show selected job-side guard results, without printing credentials:

```bash
kubectl exec -n "$RUNNER_NS" "$POD" -c guard -- \
  node --input-type=module -e '
import fs from "node:fs";
const s = JSON.parse(fs.readFileSync("/control/guard.json", "utf8"));
console.log(JSON.stringify({main_completed:s.main_completed, begin:s.begin,
  inventory:s.inventory, post_started:s.post_started}, null, 2));
'
```

The guard invokes `begin` with the coordinator assignment, sandbox root, state
path and scoped kubeconfig, then `inventory`. Its post phase invokes `cleanup`
with a remaining time budget and then `receipt`. Do not manually repeat these
mutating commands against the live job. Let the guard perform the lifecycle.

Explain that `begin` establishes scope, `inventory` records resource identities,
`cleanup` performs bounded ownership-aware deletion, and `receipt` emits unsigned
preliminary evidence. Exit code zero means a command succeeded under its checks;
external observation is still required. Read-only help/status inspection ends
when the ephemeral pod terminates. Inspect archived evidence afterward rather
than recreating a pod and presenting it as the original.

Repeat with `python3 -u work/run-walkthrough.py cleanup-blocked` to demonstrate
explicit failure and externally observed residue. The configured cleaner does
not change permissions or force Kubernetes finalizers to manufacture success.

Source: `action/src/lifecycle.ts`, `action/src/process.ts`, `cmd/proofctl/main.go`
in the repository root. The historical milestone CLI guide describes preliminary
evidence and should not be mistaken for the full AWS finalization pipeline.
