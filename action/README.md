# Guard action — milestone c

This is the Node 24 TypeScript guard. Runtime packages are **bundled**; the
runner image needs no `node_modules` directory.
The coordinator copies `dist/` to immutable `/opt/guard/dist/` and runs:

```text
node /opt/guard/dist/local.js -- COMMAND ARG ...
```

The guard calls real `/usr/local/bin/proofctl`, creates the command workspace,
inventories the assigned namespace, runs literal argv, stops its process group,
then attempts bounded cleanup and a preliminary receipt. It stages canonical
evidence and small guard status through native HTTPS to one pre-created ConfigMap.
No shell or public artifact transport is used in local mode. The GitHub adapter
uses the official Actions core toolkit and has an explicit dormant artifact mode.

Build with Node 24, TypeScript 5.9.3, esbuild 0.28.2 and the action-local lockfile:

```text
cd action
npm ci --workspaces=false --ignore-scripts --no-audit --no-fund
npm run build --workspaces=false
npm test --workspaces=false
```

Initial dependency preparation needs registry access. Subsequent builds use local
dependencies. Tests use Linux and OpenSSL, local fake proofctl fixtures and loopback
HTTPS only. Run the coordinator's separate real-kind tests for integration evidence.
Compiled ESM entrypoints and their bundled chunks are included for image builds;
copy the entire `dist/` directory and regenerate it after source edits.
`@actions/core` 3.0.1 and `@actions/artifact` 6.2.1 are pinned. Core owns GitHub
state and output handling. Artifact upload is loaded in a bounded worker only
when `PROOF_TRANSPORT=github` and `GITHUB_ACTIONS=true`; local.js rejects it.

`action.yml` declares `node24`, main/post and `post-if: always()`. The example
workflow remains outside `.github/workflows`, at
[`docs/github-workflow.yml.disabled`](../docs/github-workflow.yml.disabled).
GitHub hosted cancellation/post behavior is **unvalidated**.
See [`docs/guard.md`](../docs/guard.md) for status, configuration and boundaries.
