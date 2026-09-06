import { dirname, isAbsolute, join, resolve, relative, sep } from 'node:path';

export interface Config {
  ctl: string; assignment: string; kubeconfig: string; state: string;
  sandboxRoot: string; status: string; evidence: string;
  transport: 'kubernetes' | 'github'; stageNamespace: string; stageName: string;
  apiHost: string; apiPort: number; ca: string; token: string;
  jobTimeoutMs: number; cleanupTimeoutMs: number; ctlTimeoutMs: number;
  graceMs: number; stageTimeoutMs: number; cleanupAttempts: number;
}
function duration(env: NodeJS.ProcessEnv, key: string, fallback: number, max: number): number {
  const value = env[key] === undefined ? fallback : Number(env[key]);
  if (!Number.isSafeInteger(value) || value < 1 || value > max) throw new Error(`Invalid ${key}`);
  return value;
}
export function configFromEnv(env: NodeJS.ProcessEnv = process.env): Config {
  const state = env.PROOF_STATE ?? '/control/context.json';
  const c: Config = {
    ctl: env.PROOF_CTL ?? '/usr/local/bin/proofctl',
    assignment: env.PROOF_ASSIGNMENT ?? '/assignment/assignment.json',
    kubeconfig: env.PROOF_KUBECONFIG ?? '/assignment/kubeconfig', state,
    sandboxRoot: env.PROOF_SANDBOX_ROOT ?? '/data',
    status: join(dirname(state), 'guard.json'), evidence: join(dirname(state), 'evidence.json'),
    transport: (env.PROOF_TRANSPORT ?? 'kubernetes') as Config['transport'],
    stageNamespace: env.PROOF_STAGE_NAMESPACE ?? '', stageName: env.PROOF_STAGE_NAME ?? 'guard-stage',
    apiHost: env.KUBERNETES_SERVICE_HOST ?? '',
    apiPort: duration(env, 'KUBERNETES_SERVICE_PORT', 443, 65535),
    ca: '/var/run/secrets/kubernetes.io/serviceaccount/ca.crt',
    token: '/var/run/secrets/kubernetes.io/serviceaccount/token',
    jobTimeoutMs: duration(env, 'PROOF_JOB_TIMEOUT_MS', 900_000, 86_400_000),
    cleanupTimeoutMs: duration(env, 'PROOF_CLEANUP_TIMEOUT_MS', 120_000, 600_000),
    ctlTimeoutMs: 35_000, graceMs: 2000, stageTimeoutMs: 10_000, cleanupAttempts: 2,
  };
  validateConfig(c);
  return c;
}
export function validateConfig(c: Config): void {
  if (c.transport !== 'kubernetes' && c.transport !== 'github') throw new Error('Unsupported guard transport');
  if (c.transport === 'github' && process.env.GITHUB_ACTIONS !== 'true') throw new Error('GitHub transport requires GitHub Actions');
  for (const path of [c.ctl, c.assignment, c.kubeconfig, c.state, c.sandboxRoot, c.status, c.evidence, c.ca, c.token]) {
    if (!isAbsolute(path) || /[\r\n\0]/.test(path)) throw new Error('Guard paths must be absolute and single-line');
  }
  for (const path of [c.state, c.status, c.evidence]) {
    const rel = relative(resolve(c.sandboxRoot), resolve(path));
    if (rel === '' || (!rel.startsWith(`..${sep}`) && rel !== '..' && !isAbsolute(rel))) {
      throw new Error('Control files must be outside the sandbox root');
    }
  }
  if (c.transport === 'github') return;
  const dns = /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/;
  if (!dns.test(c.stageNamespace) || c.stageNamespace.length > 63 || !dns.test(c.stageName) || c.stageName.length > 63) {
    throw new Error('Invalid staging namespace/name');
  }
  if (!c.apiHost || /[\s/@?#]/.test(c.apiHost)) throw new Error('Invalid Kubernetes service host');
}
