import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, writeFile, readFile, rm, chmod } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawn } from 'node:child_process';
import { setTimeout as delay } from 'node:timers/promises';
import { main, post, newStatus, validateEvidence, githubState } from '../dist/lifecycle.js';
import { checkPayload } from '../dist/staging.js';
import { configFromEnv } from '../dist/config.js';
import { validateAssignment } from '../dist/assignment.js';

const here = dirname(fileURLToPath(import.meta.url));
const linux = process.platform !== 'win32';
async function fixture(t, mode = '') {
  const root = await mkdtemp(join(tmpdir(), 'guard-test-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  const control = join(root, 'control'); await mkdir(control);
  const ctl = join(root, 'proofctl');
  // Absolute interpreter gives reproducible tests without depending on PATH.
  await writeFile(ctl, `#!${process.execPath}\n${await readFile(join(here, 'fake-proof.mjs'), 'utf8')}`.replace(/\n#!\/usr\/bin\/env node/, ''));
  await chmod(ctl, 0o755);
  const c = { ctl, assignment: join(root, 'assignment.json'), kubeconfig: join(root, 'kubeconfig'),
    state: join(control, 'context.json'), sandboxRoot: join(root, 'data'), status: join(control, 'guard.json'),
    evidence: join(control, 'evidence.json'), transport: 'kubernetes', stageNamespace: 'runner-test', stageName: 'guard-stage',
    apiHost: '127.0.0.1', apiPort: 443, ca: join(root, 'ca'), token: join(root, 'token'),
    jobTimeoutMs: 2000, cleanupTimeoutMs: 500, ctlTimeoutMs: 1000, graceMs: 50, stageTimeoutMs: 100, cleanupAttempts: 2 };
  await writeFile(join(control, 'mode'), mode);
  await writeFile(c.assignment, JSON.stringify({ kind: 'cleanup-assignment/v1', identity: { provider: 'local' } }));
  const patches = []; const stage = async (_, data) => { checkPayload(data); patches.push(structuredClone(data)); };
  return { c, patches, stage, calls: async () => (await readFile(join(control, 'calls'), 'utf8')).trim().split('\n') };
}
test('default coordinator contract and configuration rejection', () => {
  const c = configFromEnv({ PROOF_STAGE_NAMESPACE: 'runner-test', KUBERNETES_SERVICE_HOST: '10.96.0.1' });
  assert.equal(c.ctl, '/usr/local/bin/proofctl'); assert.equal(c.cleanupTimeoutMs, 120000);
  assert.throws(() => configFromEnv({ PROOF_TRANSPORT: 'github' }), /transport/);
  assert.throws(() => configFromEnv({ PROOF_JOB_TIMEOUT_MS: 'NaN' }), /TIMEOUT/);
});
test('reject oversize and noncanonical evidence before transmission', () => {
  assert.throws(() => validateEvidence('{ "kind": "cleanup-evidence/v1" }'), /canonical/);
  assert.throws(() => validateEvidence(JSON.stringify({ kind: 'cleanup-evidence/v1', padding: 'x'.repeat(900 * 1024) })), /900 KiB/);
  assert.throws(() => checkPayload({ 'other.json': '{}' }), /Unexpected/);
  assert.throws(() => checkPayload({ 'guard.json': ' '.repeat(16385) }), /limit/);
});
test('GitHub assignment binds exact provider and repository/run/attempt/job/SHA', { skip: !linux }, async t => {
  const f = await fixture(t);
  const env = { GITHUB_ACTIONS: 'true', GITHUB_REPOSITORY: 'owner/repo', GITHUB_RUN_ID: '123', GITHUB_RUN_ATTEMPT: '2', GITHUB_JOB: 'guarded', GITHUB_SHA: 'b'.repeat(40) };
  const id = { provider: 'github', repository: 'owner/repo', run_id: '123', run_attempt: 2, job_id: 'guarded', source_revision: 'b'.repeat(40) };
  const write = identity => writeFile(f.c.assignment, JSON.stringify({ kind: 'cleanup-assignment/v1', identity }));
  await write(id); await validateAssignment(f.c, 'github', env);
  await assert.rejects(validateAssignment(f.c, 'local', env), /provider/);
  for (const field of Object.keys(id)) {
    await write({ ...id, [field]: field === 'run_attempt' ? 3 : 'mismatch' });
    await assert.rejects(validateAssignment(f.c, 'github', env), /provider|mismatch/);
  }
  await write(id); await assert.rejects(validateAssignment(f.c, 'github', { ...env, GITHUB_ACTIONS: 'false' }), /GitHub Actions/);
  await assert.rejects(validateAssignment(f.c, 'github', { ...env, GITHUB_RUN_ATTEMPT: '02' }), /run attempt/);
});
test('wrong-provider assignment fails main before proofctl or staging', { skip: !linux }, async t => {
  const f = await fixture(t);
  await writeFile(f.c.assignment, JSON.stringify({ kind: 'cleanup-assignment/v1', identity: { provider: 'github' } }));
  await assert.rejects(main(f.c, { stage: f.stage }), /provider/);
  assert.equal(f.patches.length, 0); await assert.rejects(f.calls(), { code: 'ENOENT' });
});
test('success stages main readiness and canonical evidence, state continuity', { skip: !linux }, async t => {
  const f = await fixture(t); const s = newStatus();
  const env = await main(f.c, { stage: f.stage }, s);
  assert.equal(env.PROOF_WORKSPACE, join(f.c.sandboxRoot, 'proof-' + 'a'.repeat(32), 'workspace'));
  assert.equal(JSON.parse(f.patches[0]['guard.json']).main_started, true);
  assert.equal(JSON.parse(f.patches[0]['guard.json']).main_completed, false);
  assert.equal(JSON.parse(f.patches[1]['guard.json']).main_completed, true);
  const result = await post(f.c, { stage: f.stage });
  assert.equal(result.cleanup_exit_code, 0); assert.equal(result.post_completed, true); assert.deepEqual(result.errors, []);
  assert.deepEqual(await f.calls(), ['begin', 'inventory', 'cleanup', 'receipt']);
  assert.equal(f.patches.at(-1)['evidence.json'], await readFile(f.c.evidence, 'utf8'));
  const github = join(dirname(f.c.status), 'github-state'); await writeFile(github, '');
  const previous = process.env.GITHUB_STATE; process.env.GITHUB_STATE = github;
  try { await githubState(f.c); } finally { if (previous === undefined) delete process.env.GITHUB_STATE; else process.env.GITHUB_STATE = previous; }
  const lines = (await readFile(github, 'utf8')).split(/\r?\n/);
  assert.match(lines[0], /^guard_config<</); assert.deepEqual(JSON.parse(lines[1]), f.c);
});
for (const mode of ['cleanup-failure', 'cleanup-timeout', 'receipt-failure', 'inventory-failure', 'begin-failure']) {
  test(`${mode} still finishes post and attempts receipt`, { skip: !linux }, async t => {
    const f = await fixture(t, mode); const s = newStatus();
    try { await main(f.c, { stage: f.stage }, s); } catch { /* expected main failures */ }
    const result = await post(f.c, { stage: f.stage }, s);
    assert.equal(result.post_completed, true); assert.ok((await f.calls()).includes('receipt'));
    assert.ok(result.errors.length);
    if (mode === 'cleanup-timeout') assert.equal(result.cleanup_timed_out, true);
    if (mode === 'cleanup-failure') { assert.equal(result.cleanup_exit_code, 7); assert.equal(result.cleanup_results.length, 2); }
  });
}
test('staging rejection explicit, retries retain failure, cleanup and receipt still run', { skip: !linux }, async t => {
  const f = await fixture(t); const s = newStatus();
  await main(f.c, { stage: f.stage }, s);
  let calls = 0;
  const stage = async (_, data) => { calls++; if (calls < 3) throw new Error('Staging PATCH rejected: HTTP 403'); f.patches.push(data); };
  const result = await post(f.c, { stage }, s);
  assert.equal(result.staging_errors.length, 2); assert.equal(result.cleanup_exit_code, 0);
  assert.equal(JSON.parse(f.patches.at(-1)['guard.json']).staging_errors.length, 2);
});
async function wrapper(t, command, opts = {}) {
  const f = await fixture(t, opts.mode); Object.assign(f.c, opts.config);
  const path = join(dirname(f.c.status), 'config.json'); await writeFile(path, JSON.stringify(f.c));
  const child = spawn(process.execPath, [join(here, 'local-harness.mjs'), path, ...command], { stdio: ['ignore', 'ignore', 'pipe'] });
  let stderr = ''; child.stderr.on('data', chunk => { stderr += chunk; });
  const done = new Promise((resolve, reject) => { child.on('error', reject); child.on('exit', (code, signal) => resolve({ code, signal })); });
  t.after(() => { if (child.exitCode === null) child.kill('SIGKILL'); });
  return { ...f, child, done, stderr: () => stderr, status: async () => JSON.parse(await readFile(f.c.status, 'utf8')) };
}
test('wrapper preserves failed command exit separately from failed cleanup', { skip: !linux }, async t => {
  const f = await wrapper(t, [process.execPath, '-e', 'process.exit(23)'], { mode: 'cleanup-failure' });
  assert.equal((await f.done).code, 23);
  const s = await f.status(); assert.equal(s.command_exit_code, 23); assert.equal(s.cleanup_exit_code, 7); assert.equal(s.post_completed, true);
});
test('command spawn failure still reaches cleanup and receipt', { skip: !linux }, async t => {
  const f = await wrapper(t, ['/no-such-guard-test-command']);
  assert.equal((await f.done).code, 1);
  const s = await f.status(); assert.equal(s.command_exit_code, null); assert.equal(s.post_completed, true);
  assert.ok(s.errors.includes('Unable to spawn user command')); assert.ok((await f.calls()).includes('receipt'));
});
test('abrupt wrapper death leaves post absent for an outside observer', { skip: !linux }, async t => {
  const f = await wrapper(t, [process.execPath, '-e', 'setTimeout(()=>{},1500)']);
  for (let i = 0; i < 200; i++) { try { if ((await f.status()).main_completed) break; } catch {} await delay(10); }
  f.child.kill('SIGKILL'); assert.equal((await f.done).signal, 'SIGKILL');
  // The short-lived orphan expires independently; the dead guard cannot finalize.
  await delay(1600);
  const s = await f.status(); assert.equal(s.post_started, false); assert.equal(s.post_completed, false);
  assert.ok(!(await f.calls()).includes('receipt'));
});
test('wrapper passes literal argv, fresh cwd and command environment', { skip: !linux }, async t => {
  const script = 'if(process.cwd()!==process.env.PROOF_WORKSPACE || !process.env.PROOF_CREDENTIALS || !process.env.PROOF_NAMESPACE || process.argv[1]!=="$(echo unsafe);*") process.exit(9)';
  const f = await wrapper(t, [process.execPath, '-e', script, '$(echo unsafe);*']);
  assert.equal((await f.done).code, 0, f.stderr());
});
test('job timeout kills ignoring command and still runs post', { skip: !linux }, async t => {
  const f = await wrapper(t, [process.execPath, '-e', 'process.on("SIGTERM",()=>{});setInterval(()=>{},1000)'], { config: { jobTimeoutMs: 120 } });
  assert.equal((await f.done).code, 124); const s = await f.status();
  assert.equal(s.command_timed_out, true); assert.equal(s.command_signal, 'SIGKILL'); assert.equal(s.post_completed, true);
});
for (const signal of ['SIGTERM', 'SIGINT']) test(`${signal} once, repeated signals during cleanup cannot abort receipt`, { skip: !linux }, async t => {
  const f = await wrapper(t, [process.execPath, '-e', 'process.on("SIGTERM",()=>{});process.on("SIGINT",()=>{});setInterval(()=>{},1000)'], { mode: 'cleanup-timeout' });
  for (let i = 0; i < 200; i++) { try { if ((await f.status()).main_completed) break; } catch {} await delay(10); }
  await delay(70); f.child.kill(signal);
  for (let i = 0; i < 200; i++) { if ((await f.status()).post_started) break; await delay(10); }
  f.child.kill('SIGTERM'); f.child.kill('SIGINT');
  assert.ok((await f.done).code >= 128); const s = await f.status();
  assert.equal(s.cancellation_signal, signal); assert.equal(s.post_completed, true); assert.ok((await f.calls()).includes('receipt'));
});
test('ordinary exit kills lingering process-group descendants before post', { skip: !linux }, async t => {
  const markerRoot = await mkdtemp(join(tmpdir(), 'guard-descendant-')); t.after(() => rm(markerRoot, { recursive: true, force: true }));
  const marker = join(markerRoot, 'late-write');
  const descendant = `process.on('SIGTERM',()=>{});setTimeout(()=>require('fs').writeFileSync(${JSON.stringify(marker)},'escaped'),600);`;
  const script = `require('child_process').spawn(process.execPath,['-e',${JSON.stringify(descendant)}],{stdio:'ignore'}).unref()`;
  const f = await wrapper(t, [process.execPath, '-e', script]); assert.equal((await f.done).code, 0, f.stderr());
  await delay(750); await assert.rejects(readFile(marker), { code: 'ENOENT' });
});
