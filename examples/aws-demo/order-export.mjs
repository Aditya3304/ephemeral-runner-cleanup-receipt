// Synthetic data-processing CI workload. It attempts no cleanup itself:
// the native action post, node observer and finalizer demonstrate that lifecycle.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import {setTimeout as delay} from 'node:timers/promises';

export function summarize(orders) {
  let cents = 0;
  for (const order of orders) {
    assert.ok(Number.isSafeInteger(order.quantity) && order.quantity > 0, 'positive integer quantity required');
    assert.ok(Number.isSafeInteger(order.unitCents) && order.unitCents >= 0, 'nonnegative integer cents required');
    cents += order.quantity * order.unitCents;
    assert.ok(Number.isSafeInteger(cents), 'total exceeds integer precision');
  }
  return {orders: orders.length, totalCents: cents};
}

async function main() {
  const scenario = process.env.DEMO_SCENARIO || 'normal';
  assert.ok(['normal', 'test-failure', 'cleanup-blocked'].includes(scenario), 'unknown experiment');
  const seconds = Number(process.env.DEMO_OBSERVATION_SECONDS ?? '45');
  assert.ok([0, 15, 45].includes(seconds), 'unsupported observation pause');
  const workspace = process.env.PROOF_WORKSPACE || '';
  const credentials = process.env.PROOF_CREDENTIALS || '';
  assert.match(workspace, /^\/data\/proof-[a-f0-9]{32}\/workspace$/);
  assert.equal(credentials, path.join(path.dirname(workspace), 'credentials'));
  assert.equal(await fs.realpath(workspace), workspace);
  assert.equal(await fs.realpath(credentials), credentials);
  const orders = [
    {customer: 'synthetic-001', quantity: 2, unitCents: 1250},
    {customer: 'synthetic-002', quantity: 1, unitCents: 999},
    {customer: 'synthetic-003', quantity: 3, unitCents: 500},
  ];
  const result = summarize(orders);
  await fs.writeFile(path.join(workspace, 'synthetic-orders.json'), JSON.stringify(orders, null, 2), {flag: 'wx', mode: 0o600});
  await fs.writeFile(path.join(workspace, 'export-summary.json'), JSON.stringify(result, null, 2), {flag: 'wx', mode: 0o600});
  await fs.writeFile(path.join(credentials, 'synthetic-service-token'), 'DEMO-ONLY-NOT-A-VALID-CREDENTIAL\n', {flag: 'wx', mode: 0o600});
  console.log(JSON.stringify({stage: 'files-created', scenario, workspace, credentials, result, run: process.env.GITHUB_RUN_ID, attempt: process.env.GITHUB_RUN_ATTEMPT}));
  if (scenario === 'cleanup-blocked') {
    const blocked = path.join(workspace, 'deliberate-cleanup-obstruction');
    await fs.mkdir(blocked, {mode: 0o700});
    await fs.writeFile(path.join(blocked, 'synthetic-residue.txt'), 'Synthetic residue for an explicit negative cleanup test.\n', {mode: 0o600});
    // Read/execute remain possible, but the non-root guard cannot unlink children.
    // This touches only a newly created child of the assigned disposable workspace.
    await fs.chmod(blocked, 0o500);
    console.log('Controlled fault: a synthetic child directory is not writable. The observer must not report workspace cleanup verified.');
  }
  console.log(`Observation window: ${seconds}s. Inspect the running pod and the listed synthetic files now.`);
  await delay(seconds * 1000);
  const expected = scenario === 'test-failure' ? 5000 : 4999;
  console.log(`Independent expected total: ${expected} cents; calculated: ${result.totalCents} cents.`);
  assert.equal(result.totalCents, expected, scenario === 'test-failure' ? 'deliberately incorrect test expectation' : 'order export total');
  console.log('Application assertion passed. This does not establish cleanup: await native post, external observation and signed verification.');
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  main().catch(error => { console.error(error.message); process.exitCode = 1; });
}
