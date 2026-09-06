import { spawn } from 'node:child_process';
import { StringDecoder } from 'node:string_decoder';
import type { Config } from './config.js';

export interface ProcessResult {
  exit_code: number | null; signal: NodeJS.Signals | null; timed_out: boolean;
  error?: string;
}
export interface ProofResult extends ProcessResult { stdout: string }
export function killGroup(pid: number | undefined, signal: NodeJS.Signals): void {
  if (!pid) return;
  try { process.kill(process.platform === 'win32' ? pid : -pid, signal); }
  catch (error) { if ((error as NodeJS.ErrnoException).code !== 'ESRCH') throw error; }
}
export function successful(result: ProcessResult): boolean {
  return result.exit_code === 0 && !result.signal && !result.timed_out && !result.error;
}
export function runProof(c: Config, args: string[], timeoutMs: number): Promise<ProofResult> {
  return new Promise(resolve => {
    let stdout = ''; let bytes = 0; let timed_out = false; let error: string | undefined;
    let done = false; const decoder = new StringDecoder('utf8');
    let reapTimer: ReturnType<typeof setTimeout> | undefined;
    const child = spawn(c.ctl, [...args, '--state', c.state, '--kubeconfig', c.kubeconfig], {
      shell: false, detached: process.platform !== 'win32', windowsHide: true,
      stdio: ['ignore', 'pipe', 'ignore'],
      // Never allow job-controlled preload flags to inject code into a helper.
      env: { ...process.env, NODE_OPTIONS: '', NODE_PATH: '' },
    });
    const finish = (exit_code: number | null, signal: NodeJS.Signals | null) => {
      if (done) return; done = true; clearTimeout(timer); clearTimeout(reapTimer);
      try { killGroup(child.pid, 'SIGKILL'); } catch { error ??= 'Unable to kill proofctl process group'; }
      child.stdout.destroy();
      resolve({ exit_code, signal, timed_out, ...(error ? { error } : {}), stdout: stdout + decoder.end() });
    };
    const timer = setTimeout(() => {
      timed_out = true;
      try { killGroup(child.pid, 'SIGKILL'); } catch { error = 'Unable to kill timed-out proofctl'; }
      // Reap the helper before receipt tries to acquire its context lock, but
      // bound even an abnormal OS/pipe failure after the kill request.
      reapTimer = setTimeout(() => finish(null, 'SIGKILL'), 1000);
    }, timeoutMs);
    child.stdout.on('data', (chunk: Buffer) => {
      bytes += chunk.length;
      if (bytes > 1024 * 1024) { error = 'proofctl output exceeds 1 MiB'; finish(null, 'SIGKILL'); }
      else stdout += decoder.write(chunk);
    });
    child.on('error', () => { error = 'Unable to execute proofctl'; finish(null, null); });
    child.on('exit', (code, signal) => {
      try { killGroup(child.pid, 'SIGKILL'); } catch { error = 'Unable to kill proofctl descendants'; }
      // close drains stdout; the hard deadline remains active until then.
    });
    child.on('close', finish);
  });
}
