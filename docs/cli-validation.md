# Milestone b validation

Validated locally in Ubuntu/WSL on September 7, 2026. No paid infrastructure was
used. All resource cleanup ran against the dedicated local kind cluster.

## Actual results

| Check | Result |
|---|---|
| Cached CLI build | Passed with Go 1.27.1 |
| Filesystem cleanup | Hidden/nested files removed; symlink and hardlink targets preserved; repeat cleanup succeeds |
| Ownership and bounds | Replaced directory/marker, changed namespace UID/label or cluster UID rejected; cancellation preserves residual evidence |
| State and evidence | Strict JSON, canonical output, exact large inode/device identities, sticky failures and unsigned partial verdict passed |
| Input safety | Special files rejected; state/evidence paths cannot recreate files inside the disposable workspace |
| Real offline Sigstore | Genuine upstream bundle accepted with exact identity, issuer, digest and independent trust root |
| Sigstore rejection cases | Wrong digest, identity, issuer, artifact, signature and trust root all rejected |
| Live Kubernetes resources | Namespace, service account, secret, bound PVC and owned PV removed; unrelated namespace and external file preserved |
| Live stuck finalizer | Deadline exceeded as expected; cleanup did not strip the finalizer |
| Live ownership change | Changed namespace label blocked deletion |
| Live object replacement | Same-name object with a new UID preserved; cleanup rejected the stale inventory |
| Actual CLI lifecycle | begin, inventory, cleanup, repeated cleanup, receipt and separate verify command passed |
| Static analysis | go vet -p 2 ./... passed |

`bash dev cli-check` executes the unit and real offline signature checks. Its live
cluster test is intentionally skipped there; `bash dev kind-check` separately ran
all four real cluster scenarios and passed (18.72 seconds). The test harness
removed only its own test resources afterward, including its injected finalizer.

The final CLI demonstration used namespace
`proof-5767b5573124d264d2ac02a7fa079712`, inventoried five resources and confirmed
their absence. Both disposable directories were empty and an external sentinel
survived. Its evidence SHA-256 is:

```text
4422a8bc478b58d248713b13406773b4436e5327d6431881a830b71e33ed4c42
```

The generated file is retained under
`.build/demo/1788730118995882386/cleanup-evidence.json`, with a byte-identical
user-facing copy alongside the repository in the Codex outputs directory.
The source revision in this demonstration is the committed foundation revision;
the CLI changes were under validation before their milestone commit.

## Scope of this evidence

This is preliminary job-observed evidence. It is **unsigned and partial**, because
logs and runner disposal have not been observed by an independent finalizer.
No synthetic or preliminary receipt was inserted into PostgreSQL. The independent
Cosign demo verifies a pre-signed upstream test fixture; it does not sign or verify
the new cleanup evidence. Private local signing remains milestone d.

PV/PVC tests establish Kubernetes object absence, not forensic storage erasure.
The local developer cluster is not yet a hardened environment for hostile CI jobs.
End-to-end signed receipts, a dashboard and execution with external networking
blocked are later acceptance gates, and are not claimed by this milestone.
