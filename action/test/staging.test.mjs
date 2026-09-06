import test from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:https';
import { execFileSync } from 'node:child_process';
import { mkdtemp, writeFile, readFile, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { stageKubernetes } from '../dist/staging.js';

test('native HTTPS uses exact PATCH map, verified CA and token; rejects HTTP errors and bounds timeout', { skip: process.platform === 'win32' }, async t => {
  const root = await mkdtemp(join(tmpdir(), 'guard-tls-')); t.after(() => rm(root, { recursive: true, force: true }));
  const key = join(root, 'server.key'); const cert = join(root, 'server.crt'); const token = join(root, 'token');
  // Disposable test-only credentials, generated locally; never production fixtures.
  execFileSync('openssl', ['req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-keyout', key, '-out', cert, '-days', '1', '-subj', '/CN=invalid.example', '-addext', 'subjectAltName=IP:127.0.0.1'], { stdio: 'ignore' });
  await writeFile(token, 'unit-test-token\n');
  let code = 200; let hang = false; let received;
  const sockets = new Set();
  const server = createServer({ key: await readFile(key), cert: await readFile(cert) }, async (req, res) => {
    let raw = ''; for await (const chunk of req) raw += chunk;
    received = { method: req.method, url: req.url, auth: req.headers.authorization, contentType: req.headers['content-type'], body: JSON.parse(raw) };
    if (!hang) { res.writeHead(code); res.end('{}'); }
  });
  server.on('connection', socket => { sockets.add(socket); socket.on('close', () => sockets.delete(socket)); });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(async () => { for (const socket of sockets) socket.destroy(); await new Promise(resolve => server.close(resolve)); });
  const c = { apiHost: '127.0.0.1', apiPort: server.address().port, ca: cert, token, stageNamespace: 'runner-test', stageName: 'guard-stage', stageTimeoutMs: 1000 };
  const data = { 'evidence.json': '{"kind":"cleanup-evidence/v1"}', 'guard.json': '{"main_started":true}' };
  await stageKubernetes(c, data);
  assert.deepEqual(received, { method: 'PATCH', url: '/api/v1/namespaces/runner-test/configmaps/guard-stage', auth: 'Bearer unit-test-token', contentType: 'application/merge-patch+json', body: { data } });
  for (const errorCode of [302, 401, 403, 404, 413]) { code = errorCode; await assert.rejects(stageKubernetes(c, data), new RegExp(`HTTP ${errorCode}`)); }
  hang = true; c.stageTimeoutMs = 60;
  const started = Date.now(); await assert.rejects(stageKubernetes(c, data), /timed out/); assert.ok(Date.now() - started < 1000);
  hang = false; c.stageTimeoutMs = 1000; c.apiHost = 'localhost';
  await assert.rejects(stageKubernetes(c, data), /HTTPS connection failed/);
});
