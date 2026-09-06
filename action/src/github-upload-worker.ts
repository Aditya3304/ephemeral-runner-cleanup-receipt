import { parentPort, workerData } from 'node:worker_threads';
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import type { Config } from './config.js';
import { checkPayload } from './staging.js';
import { validateAssignment } from './assignment.js';

let ok = false;
let directory: string | undefined;
try {
  const { config, data } = workerData as { config: Config; data: Record<string, string> };
  if (!parentPort || config.transport !== 'github' || process.env.GITHUB_ACTIONS !== 'true') throw new Error('GitHub-only worker');
  checkPayload(data);
  await validateAssignment(config, 'github');
  const artifact = await import('@actions/artifact');
  await mkdir(dirname(config.status), { recursive: true });
  directory = await mkdtemp(join(dirname(config.status), 'guard-upload-'));
  const files: string[] = [];
  for (const [key, value] of Object.entries(data)) { const file = join(directory, key); await writeFile(file, value, { mode: 0o600 }); files.push(file); }
  const identity = [process.env.GITHUB_RUN_ID, process.env.GITHUB_RUN_ATTEMPT, process.env.GITHUB_JOB].join('-').replace(/[^a-zA-Z0-9_-]/g, '_').slice(0, 120);
  const result = await new artifact.DefaultArtifactClient().uploadArtifact(`cleanup-preliminary-${identity}`, files, directory, { retentionDays: 1 });
  if (!result.id) throw new Error('Artifact upload returned no ID');
  ok = true;
} catch { ok = false; }
finally { if (directory) await rm(directory, { recursive: true, force: true }); }
parentPort?.postMessage({ ok });
