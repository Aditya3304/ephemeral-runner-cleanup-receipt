import { Worker } from 'node:worker_threads';
import type { Stage } from './staging.js';
import { checkPayload } from './staging.js';

export const stageGithub: Stage = async (config, data) => {
  if (config.transport !== 'github' || process.env.GITHUB_ACTIONS !== 'true') throw new Error('Artifact upload requires explicit GitHub transport');
  checkPayload(data);
  // A worker gives the toolkit a real cancellation boundary. A Promise.race
  // alone would leave its retry timers and network requests alive after timeout.
  await new Promise<void>((resolve, reject) => {
    const worker = new Worker(new URL('./github-upload-worker.js', import.meta.url), { workerData: { config, data } });
    let done = false;
    const finish = async (error?: Error) => {
      if (done) return; done = true; clearTimeout(timer);
      await worker.terminate(); if (error) reject(error); else resolve();
    };
    const timer = setTimeout(() => void finish(new Error('GitHub artifact upload timed out')), 35_000);
    worker.once('message', message => void finish(message.ok ? undefined : new Error('GitHub artifact upload failed')));
    worker.once('error', () => void finish(new Error('GitHub artifact worker failed')));
    worker.once('exit', () => { if (!done) void finish(new Error('GitHub artifact worker exited without confirmation')); });
  });
};
