# Milestone c guard contract

The Node 24 guard emits **untrusted, unsigned preliminary evidence**. It does not
sign receipts, access an archive/database, or prove runner disposal. The coordinator
must independently observe Pod termination and handle absent post observations.

## Runtime and interfaces

Mount compiled `action/dist` read-only at `/opt/guard/dist`; use the real CLI at
`/usr/local/bin/proofctl`. Runtime packages are bundled inside `dist`, so no
`node_modules` copy is required. Linux Node
24.20.0 is the integration runtime. TypeScript is pinned to 5.9.3 and Node types
to 24.10.1, esbuild to 0.28.2, Actions core to 3.0.1 and artifact to 6.2.1,
with an action-local lockfile. ESM bundles use a `createRequire` banner for
CommonJS dependencies and `dist/package.json` declares ESM. Copy all dist chunks.
`main.js` and `post.js` are GitHub
entrypoints; `local.js -- COMMAND ARG ...` is the coordinator's PID 1 entrypoint.
All three use `lifecycle.js` functions `main(config)` and `post(config)`.

| Environment | Default / requirement |
|---|---|
| `PROOF_CTL` | `/usr/local/bin/proofctl` |
| `PROOF_ASSIGNMENT` | `/assignment/assignment.json` |
| `PROOF_KUBECONFIG` | `/assignment/kubeconfig` |
| `PROOF_STATE` | `/control/context.json` |
| `PROOF_SANDBOX_ROOT` | `/data` |
| `PROOF_TRANSPORT` | `kubernetes`; optional `github` only inside GitHub Actions |
| `PROOF_STAGE_NAMESPACE` | Kubernetes transport requires runner namespace |
| `PROOF_STAGE_NAME` | `guard-stage` |
| `KUBERNETES_SERVICE_HOST` | Kubernetes transport requires HTTPS service host |
| `KUBERNETES_SERVICE_PORT` | `443` |
| `PROOF_JOB_TIMEOUT_MS` | `900000`, at most one day |
| `PROOF_CLEANUP_TIMEOUT_MS` | `120000`, at most ten minutes total |

Projected CA and token are read from
`/var/run/secrets/kubernetes.io/serviceaccount/{ca.crt,token}`. The kubeconfig
uses a projected token file; no raw token is saved by the guard. Control files
remain outside the disposable root. Main runs:

```text
proofctl begin --assignment FILE --sandbox-root PATH --state STATE --kubeconfig FILE
proofctl inventory --state STATE --kubeconfig FILE
```

Before begin, local mode requires a local-provider assignment. The GitHub adapter
requires provider `github` and exact equality with `GITHUB_REPOSITORY`,
`GITHUB_RUN_ID`, numeric `GITHUB_RUN_ATTEMPT`, `GITHUB_JOB` and `GITHUB_SHA`.
GitHub post and its upload worker revalidate that binding before upload.
It validates the returned `cleanup-context/v1` State namespace/token/sandbox
relationship and returns `PROOF_WORKSPACE`, `PROOF_CREDENTIALS`, and
`PROOF_NAMESPACE`. The command runs with those variables and its current directory
set to the new workspace. It receives the exact argv after `--`, with `shell:false`.
The local wrapper uses a detached Linux process group, forwards the first SIGTERM
or SIGINT, allows two seconds of grace, and kills that group before post. It also
kills remaining descendants after ordinary command exit. Repeated signals cannot
interrupt cleanup or its retry. An early signal during main suppresses the command
and proceeds to post after bounded main operations finish.

Post tries cleanup up to twice within one shared deadline, then always tries
`proofctl receipt` with a separate 35-second deadline. A helper may need up to one
additional second to reap after SIGKILL. Each HTTPS staging operation has a hard
10-second deadline, and the final PATCH is retried once. A failed cleanup attempt
remains recorded even if the retry succeeds. `cleanup_exit_code` is the final
attempt's code; `cleanup_results` retains every attempt. Helpers have bounded
stdout and no inherited stdin. Evidence is checked against canonical sorted-key
JSON serialization and preserved byte-for-byte as a string, never parsed into
the ConfigMap data value.

## Status and staging

Status is persisted beside the context as `/control/guard.json`; evidence is
`/control/evidence.json`. The staging ConfigMap has exactly the guard-owned keys
`data["guard.json"]` and `data["evidence.json"]`. Initial main status and completed
main status are PATCHed early; the coordinator can wait for `main_completed`.
Post stages its start and completion. Evidence is at most **900 KiB** and status
at most **16 KiB**. JSON merge PATCH targets one exact namespace/map through native
HTTPS with certificate verification; redirects, missing maps and RBAC rejections
are explicit failures. The guard never creates a map or broadens its target.

The staging role should grant only `get` and `patch` on ConfigMaps with
`resourceNames: [guard-stage]` in the runner namespace; no list/create permission.
The guard itself only needs PATCH. Resource cleanup permissions belong to the
separate assigned disposable namespace.

The status JSON has this shape (null means no observed process result):

```json
{
  "kind": "cleanup-guard/v1",
  "main_started": true,
  "main_completed": true,
  "post_started": true,
  "post_completed": true,
  "command_exit_code": 0,
  "command_signal": null,
  "command_timed_out": false,
  "cancellation_signal": null,
  "cleanup_exit_code": 0,
  "cleanup_signal": null,
  "cleanup_timed_out": false,
  "cleanup_results": [{"exit_code": 0, "signal": null, "timed_out": false}],
  "begin": {"exit_code": 0, "signal": null, "timed_out": false},
  "inventory": {"exit_code": 0, "signal": null, "timed_out": false},
  "receipt": {"exit_code": 0, "signal": null, "timed_out": false},
  "staging_errors": [],
  "errors": []
}
```

Per-process results may also contain `error`. Boolean completion fields indicate
lifecycle progress, not a cleanup verdict or proof that delivery succeeded. A
failed staging request is added to `staging_errors`, persisted locally and printed
to stderr. If retry succeeds, those earlier errors are included in staged status.
If every PATCH fails, there can be no remote confirmation of the final failure:
the local file/log and independent coordinator observations are the fallback.

Wrapper exit policy preserves nonzero command codes first, then uses 124 for a
job timeout, 128+signal for cancellation/signal termination, and 1 for guard-only
failure. Status keeps the actual command exit/signal and cleanup exit separately.

## GitHub adapter and observation limits

`action/action.yml` declares `using: node24`, `main: dist/main.js`,
`post: dist/post.js`, and `post-if: always()`. Main writes its configuration to
`GITHUB_STATE` before begin, and post restores `STATE_guard_config`. Main publishes
environment values through `GITHUB_ENV` and corresponding lowercase action outputs
through `GITHUB_OUTPUT`, using official `@actions/core` functions `saveState`,
`getState`, `exportVariable`, `setOutput` and `setFailed`. The disabled workflow uses an externally provisioned
read-only action directory and minimal `permissions: {}`. It selects the dormant
GitHub artifact transport described below. It needs no OIDC token,
archive credentials, database credentials, or checkout action. An external
controller provisions the immutable action and a matching assignment; this is
setup documentation only, not an active or validated hosted workflow.

An optional `PROOF_TRANSPORT=github` mode is implemented for a future hosted
adapter. It requires `GITHUB_ACTIONS=true` and is rejected by `local.js`, even if
that environment variable is present. The official `@actions/artifact` client is
dynamically loaded in a worker only for final post upload; early status is local
in this mode. It uploads status and available preliminary evidence as a one-day
artifact named from the GitHub run, attempt and job. The worker is terminated after
35 seconds so toolkit retries cannot outlive that deadline. No live artifact upload
has been validated, and no hosted workflow is enabled.

GitHub-hosted cancellation and post execution are **unvalidated**. The GitHub
main/post adapter does not observe separate workflow commands, so its command exit
and signal remain null. The local wrapper records its owned command directly.
SIGKILL, runtime/host death, forced Pod removal or failure before guard startup may
prevent post entirely; only the outside coordinator can evidence that absence.
Process-group cleanup observes descendants that remain in that group. A malicious
process that creates a new session can escape it, and same-UID job code can tamper
with job observations. Independent Pod disposal is required; no trusted disposal
claim is inferred from these files or from a successful wrapper exit.

## Validation

The unit suite uses a fake proof executable **only under `action/test`**. It covers
success, begin/inventory/cleanup/receipt failure, cleanup timeout, command failure
and timeout, SIGTERM/SIGINT with repeated signals during post, literal argv and
workspace environment, lingering descendants, canonical/size rejection, GitHub
state continuity and staging rejection/retry. A loopback HTTPS server tests the
exact API target, token/CA handling, redirects, HTTP 401/403/404/413 and deadlines.
These tests do not replace the coordinator's real proofctl/kind integration tests.
On September 7, 2026, the TypeScript check and all 21 Linux Node 24.20.0 tests
passed, including loading copied bundles without node_modules and confirming
that the local entrypoint's static module graph excludes the artifact toolkit.

Runtime references: [Node process groups](https://nodejs.org/api/child_process.html#optionsdetached)
and [GitHub action metadata](https://docs.github.com/en/actions/reference/workflows-and-actions/metadata-syntax).
Package versions were verified against the official npm registry; the
[Actions toolkit artifact source](https://github.com/actions/toolkit/tree/main/packages/artifact)
defines the client interface. The source main branch was ahead of published npm
(6.2.2 versus 6.2.1); this guard pins published 6.2.1.
