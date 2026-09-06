#!/usr/bin/env node
// Test-only executable. Production never falls back to this fixture.
import { appendFileSync, existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
const args = process.argv.slice(2);
const command = args[0];
const flag = name => args[args.indexOf(name) + 1];
const statePath = flag('--state');
const control = dirname(statePath);
const mode = existsSync(join(control, 'mode')) ? readFileSync(join(control, 'mode'), 'utf8') : '';
appendFileSync(join(control, 'calls'), `${command}\n`);
if (mode === `${command}-timeout`) { process.on('SIGTERM', () => {}); setInterval(() => {}, 1000); }
else if (mode === `${command}-failure`) { process.exitCode = 7; }
else if (command === 'begin') {
  const token = 'a'.repeat(32); const namespace = `proof-${token}`;
  const sandbox = join(flag('--sandbox-root'), namespace);
  mkdirSync(join(sandbox, 'workspace'), { recursive: true });
  mkdirSync(join(sandbox, 'credentials'), { recursive: true });
  const state = { kind: 'cleanup-context/v1', token, namespace, sandbox };
  writeFileSync(statePath, JSON.stringify(state)); process.stdout.write(JSON.stringify(state));
} else if (command === 'receipt') {
  process.stdout.write('{"kind":"cleanup-evidence/v1","verdict":"partial"}');
} else process.stdout.write('{}');
