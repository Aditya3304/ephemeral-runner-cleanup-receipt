import { open, constants } from 'node:fs/promises';
import type { Config } from './config.js';

export async function validateAssignment(c: Config, provider: 'github' | 'local', env: NodeJS.ProcessEnv = process.env): Promise<void> {
  const file = await open(c.assignment, constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
  let bytes: Buffer;
  try {
    const stat = await file.stat();
    if (!stat.isFile() || stat.size > 16 * 1024) throw new Error('Assignment must be a bounded regular file');
    const buffer = Buffer.alloc(16 * 1024 + 1);
    const read = await file.read(buffer, 0, buffer.length, 0);
    if (read.bytesRead > 16 * 1024) throw new Error('Assignment exceeds 16 KiB');
    bytes = buffer.subarray(0, read.bytesRead);
  } finally { await file.close(); }
  const assignment = JSON.parse(bytes.toString('utf8')) as { kind?: string; identity?: Record<string, unknown> };
  const id = assignment.identity;
  if (assignment.kind !== 'cleanup-assignment/v1' || !id || id.provider !== provider) throw new Error(`Assignment provider must be ${provider}`);
  if (provider === 'local') return;
  if (env.GITHUB_ACTIONS !== 'true') throw new Error('GitHub assignment requires GitHub Actions');
  const expected = { repository: env.GITHUB_REPOSITORY, run_id: env.GITHUB_RUN_ID,
    run_attempt: Number(env.GITHUB_RUN_ATTEMPT), job_id: env.GITHUB_JOB, source_revision: env.GITHUB_SHA };
  if (!env.GITHUB_RUN_ATTEMPT || !/^[1-9][0-9]*$/.test(env.GITHUB_RUN_ATTEMPT) || !Number.isSafeInteger(expected.run_attempt)) throw new Error('Invalid GitHub run attempt');
  for (const [field, value] of Object.entries(expected)) {
    if (value === undefined || value === '' || id[field] !== value) throw new Error(`Assignment GitHub identity mismatch: ${field}`);
  }
}
