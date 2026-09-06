import test from 'node:test';
import assert from 'node:assert/strict';
import { cp, mkdtemp, readFile, rm } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { tmpdir } from 'node:os';

const action = resolve(dirname(fileURLToPath(import.meta.url)), '..');
test('dist-only runtime imports work without node_modules; local graph does not load artifact', async t => {
  const root = await mkdtemp(join(tmpdir(), 'guard-bundle-')); t.after(() => rm(root, { recursive: true, force: true }));
  await cp(join(action, 'dist'), join(root, 'dist'), { recursive: true });
  const meta = JSON.parse(await readFile(join(action, 'work/bundle-meta.json'), 'utf8'));
  const visit = (key, seen = new Set()) => {
    if (seen.has(key)) return seen; seen.add(key);
    const output = meta.outputs[key]; assert.ok(output, `Missing bundled file ${key}`);
    for (const item of output.imports) if (!item.external && item.kind !== 'dynamic-import') visit(item.path, seen);
    return seen;
  };
  for (const key of visit('dist/local.js')) {
    assert.ok(!Object.keys(meta.outputs[key].inputs).some(path => path.includes('@actions/artifact')), 'Artifact toolkit must remain dormant in local mode');
  }
  const library = pathToFileURL(join(root, 'dist/lifecycle.js')).href;
  execFileSync(process.execPath, ['--input-type=module', '-e', `const x=await import(${JSON.stringify(library)}); if(typeof x.main!=='function'||typeof x.post!=='function')process.exit(1)`], { cwd: root, stdio: 'pipe', env: { ...process.env, NODE_PATH: '', NODE_OPTIONS: '' } });
  // Importing the bundled toolkit module exercises bundled CommonJS dependencies
  // without constructing an upload request or using any external service.
  const artifact = Object.entries(meta.outputs).find(([, output]) => output.entryPoint?.includes('@actions/artifact/'));
  assert.ok(artifact, 'Expected artifact dynamic-import bundle');
  const artifactURL = pathToFileURL(join(root, artifact[0])).href;
  execFileSync(process.execPath, ['--input-type=module', '-e', `const x=await import(${JSON.stringify(artifactURL)}); if(typeof x.DefaultArtifactClient!=='function')process.exit(1)`], { cwd: root, stdio: 'pipe', env: { ...process.env, NODE_PATH: '', NODE_OPTIONS: '' } });
});
