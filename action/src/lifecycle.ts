import { mkdir, readFile, rename, writeFile } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import type { Config } from './config.js';
import { runProof, successful, type ProcessResult, type ProofResult } from './process.js';
import { MAX_EVIDENCE_BYTES, MAX_STATUS_BYTES, stageKubernetes, type Stage } from './staging.js';
import { validateAssignment } from './assignment.js';

export interface GuardStatus {
  kind: 'cleanup-guard/v1';
  main_started: boolean; main_completed: boolean; post_started: boolean; post_completed: boolean;
  command_exit_code: number | null; command_signal: NodeJS.Signals | null;
  command_timed_out: boolean; cancellation_signal: NodeJS.Signals | null;
  cleanup_exit_code: number | null; cleanup_signal: NodeJS.Signals | null;
  cleanup_timed_out: boolean; cleanup_results: ProcessResult[];
  begin: ProcessResult | null; inventory: ProcessResult | null; receipt: ProcessResult | null;
  staging_errors: string[]; errors: string[];
}
export interface Dependencies { stage?: Stage; proof?: typeof runProof }
export function newStatus(): GuardStatus {
  return { kind: 'cleanup-guard/v1', main_started: false, main_completed: false,
    post_started: false, post_completed: false, command_exit_code: null, command_signal: null,
    command_timed_out: false, cancellation_signal: null, cleanup_exit_code: null,
    cleanup_signal: null, cleanup_timed_out: false, cleanup_results: [],
    begin: null, inventory: null, receipt: null, staging_errors: [], errors: [] };
}
export function note(list: string[], message: string): void {
  if (list.length < 12) list.push(message.slice(0, 400));
}
export async function saveStatus(c: Config, status: GuardStatus): Promise<void> {
  const data = JSON.stringify(status);
  if (Buffer.byteLength(data) > MAX_STATUS_BYTES) throw new Error('Guard status exceeds 16 KiB');
  await mkdir(dirname(c.status), { recursive: true, mode: 0o700 });
  const tmp = `${c.status}.${process.pid}.tmp`;
  await writeFile(tmp, data, { mode: 0o600 });
  await rename(tmp, c.status);
}
export async function loadStatus(c: Config): Promise<GuardStatus> {
  try {
    const raw = await readFile(c.status);
    if (raw.length > MAX_STATUS_BYTES) throw new Error('Oversize guard status');
    const value = JSON.parse(raw.toString()) as GuardStatus;
    if (value.kind !== 'cleanup-guard/v1' || !Array.isArray(value.errors) || !Array.isArray(value.staging_errors) || !Array.isArray(value.cleanup_results)) throw new Error('Invalid guard status');
    return value;
  } catch {
    const status = newStatus(); note(status.errors, 'Prior guard status missing or invalid'); return status;
  }
}
function summary(result: ProofResult): ProcessResult {
  const { stdout: _, ...status } = result; return status;
}
async function persist(c: Config, s: GuardStatus): Promise<void> {
  try { await saveStatus(c, s); }
  catch { note(s.errors, 'Unable to persist guard status'); console.error('guard: unable to persist status'); }
}
async function stage(c: Config, s: GuardStatus, deps: Dependencies, evidence?: string): Promise<boolean> {
  try {
    const data = { ...(evidence === undefined ? {} : { 'evidence.json': evidence }), 'guard.json': JSON.stringify(s) };
    if (deps.stage) await deps.stage(c, data);
    else if (c.transport === 'kubernetes') await stageKubernetes(c, data);
    else if (s.post_completed) {
      const { stageGithub } = await import('./github-artifact.js');
      await stageGithub(c, data);
    }
    return true;
  } catch (error) {
    note(s.staging_errors, error instanceof Error ? error.message : 'Staging failed');
    console.error(`guard: ${s.staging_errors.at(-1)}`);
    await persist(c, s); return false;
  }
}
export async function githubState(c: Config): Promise<void> {
  const core = await import('@actions/core');
  core.saveState('guard_config', JSON.stringify(c));
}
export async function main(c: Config, deps: Dependencies = {}, status = newStatus(), provider: 'local' | 'github' = 'local'): Promise<Record<string, string>> {
  const proof = deps.proof ?? runProof;
  status.main_started = true;
  await saveStatus(c, status);
  let assignmentValid = false;
  try {
    await validateAssignment(c, provider); assignmentValid = true;
    await stage(c, status, deps);
    const begin = await proof(c, ['begin', '--assignment', c.assignment, '--sandbox-root', c.sandboxRoot], c.ctlTimeoutMs);
    status.begin = summary(begin); await persist(c, status);
    if (!successful(begin)) throw new Error('proofctl begin failed');
    const state = JSON.parse(begin.stdout) as { kind: string; sandbox: string; namespace: string; token: string };
    if (state.kind !== 'cleanup-context/v1' || !/^[a-f0-9]{32}$/.test(state.token) || state.namespace !== `proof-${state.token}` || state.sandbox !== join(resolve(c.sandboxRoot), state.namespace)) {
      throw new Error('proofctl begin returned invalid workspace identity');
    }
    const inventory = await proof(c, ['inventory'], c.ctlTimeoutMs);
    status.inventory = summary(inventory);
    if (!successful(inventory)) throw new Error('proofctl inventory failed');
    status.main_completed = true;
    await saveStatus(c, status);
    if (!await stage(c, status, deps)) throw new Error('Cannot stage main completion');
    return { PROOF_WORKSPACE: join(state.sandbox, 'workspace'), PROOF_CREDENTIALS: join(state.sandbox, 'credentials'), PROOF_NAMESPACE: state.namespace };
  } catch (error) {
    note(status.errors, error instanceof Error ? error.message : 'Guard main failed');
    await persist(c, status); if (assignmentValid) await stage(c, status, deps); throw error;
  }
}
// proofctl is the canonicalizer. Compare to ECMAScript sorted-key serialization
// instead of reserializing the bytes destined for the staging map.
function canonical(value: unknown): string {
  if (value === null || typeof value !== 'object') return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(canonical).join(',')}]`;
  return `{${Object.keys(value).sort().map(key => `${JSON.stringify(key)}:${canonical((value as Record<string, unknown>)[key])}`).join(',')}}`;
}
export function validateEvidence(raw: string): void {
  if (Buffer.byteLength(raw) > MAX_EVIDENCE_BYTES) throw new Error('Evidence exceeds 900 KiB');
  const parsed = JSON.parse(raw) as { kind?: string };
  if (parsed.kind !== 'cleanup-evidence/v1' || canonical(parsed) !== raw) throw new Error('Receipt is not canonical cleanup evidence');
}
export async function post(c: Config, deps: Dependencies = {}, given?: GuardStatus): Promise<GuardStatus> {
  const s = given ?? await loadStatus(c); const proof = deps.proof ?? runProof;
  s.post_started = true; await persist(c, s);
  await stage(c, s, deps);
  const deadline = Date.now() + c.cleanupTimeoutMs;
  try {
    for (let attempt = 0; attempt < c.cleanupAttempts; attempt++) {
      const remaining = deadline - Date.now();
      if (remaining <= 0) break;
      const result = await proof(c, ['cleanup', '--timeout', `${Math.max(1, remaining - 100)}ms`], remaining);
      s.cleanup_results.push(summary(result)); s.cleanup_exit_code = result.exit_code;
      s.cleanup_signal = result.signal; s.cleanup_timed_out ||= result.timed_out;
      await persist(c, s);
      if (successful(result)) break;
    }
    if (s.cleanup_results.length === 0 || s.cleanup_results.some(r => !successful(r))) note(s.errors, 'Cleanup failed or timed out; all attempts retained');
  } catch { note(s.errors, 'Cleanup execution failed'); }
  let evidence: string | undefined;
  // Receipt is independent of cleanup success and has its own bounded deadline.
  try {
    const result = await proof(c, ['receipt'], c.ctlTimeoutMs); s.receipt = summary(result);
    if (!successful(result)) throw new Error('proofctl receipt failed');
    validateEvidence(result.stdout); evidence = result.stdout;
    await writeFile(c.evidence, evidence, { mode: 0o600 });
  } catch (error) { note(s.errors, error instanceof Error ? error.message : 'Receipt failed'); }
  s.post_completed = true;
  await persist(c, s);
  // Retry once; a rejection remains visible even if the second PATCH succeeds.
  if (!await stage(c, s, deps, evidence)) await stage(c, s, deps, evidence);
  await persist(c, s);
  console.error(`guard-status: ${JSON.stringify(s)}`);
  return s;
}
export function guardFailed(s: GuardStatus): boolean {
  return !s.main_completed || !s.post_completed || s.errors.length > 0 || s.staging_errors.length > 0;
}
