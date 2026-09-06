import { createRequire as __guardCreateRequire } from 'node:module'; const require = __guardCreateRequire(import.meta.url);
import {
  runProof,
  successful
} from "./chunk-OKTPO7TK.js";
import {
  validateAssignment
} from "./chunk-MC4MAVAB.js";
import {
  MAX_EVIDENCE_BYTES,
  MAX_STATUS_BYTES,
  stageKubernetes
} from "./chunk-26V5X43U.js";

// src/lifecycle.ts
import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
function newStatus() {
  return {
    kind: "cleanup-guard/v1",
    main_started: false,
    main_completed: false,
    post_started: false,
    post_completed: false,
    command_exit_code: null,
    command_signal: null,
    command_timed_out: false,
    cancellation_signal: null,
    cleanup_exit_code: null,
    cleanup_signal: null,
    cleanup_timed_out: false,
    cleanup_results: [],
    begin: null,
    inventory: null,
    receipt: null,
    staging_errors: [],
    errors: []
  };
}
function note(list, message) {
  if (list.length < 12) list.push(message.slice(0, 400));
}
async function saveStatus(c, status) {
  const data = JSON.stringify(status);
  if (Buffer.byteLength(data) > MAX_STATUS_BYTES) throw new Error("Guard status exceeds 16 KiB");
  await mkdir(dirname(c.status), { recursive: true, mode: 448 });
  const tmp = `${c.status}.${process.pid}.tmp`;
  await writeFile(tmp, data, { mode: 384 });
  await rename(tmp, c.status);
}
async function loadStatus(c) {
  try {
    const raw = await readFile(c.status);
    if (raw.length > MAX_STATUS_BYTES) throw new Error("Oversize guard status");
    const value = JSON.parse(raw.toString());
    if (value.kind !== "cleanup-guard/v1" || !Array.isArray(value.errors) || !Array.isArray(value.staging_errors) || !Array.isArray(value.cleanup_results)) throw new Error("Invalid guard status");
    return value;
  } catch {
    const status = newStatus();
    note(status.errors, "Prior guard status missing or invalid");
    return status;
  }
}
function summary(result) {
  const { stdout: _, ...status } = result;
  return status;
}
async function persist(c, s) {
  try {
    await saveStatus(c, s);
  } catch {
    note(s.errors, "Unable to persist guard status");
    console.error("guard: unable to persist status");
  }
}
async function stage(c, s, deps, evidence) {
  try {
    const data = { ...evidence === void 0 ? {} : { "evidence.json": evidence }, "guard.json": JSON.stringify(s) };
    if (deps.stage) await deps.stage(c, data);
    else if (c.transport === "kubernetes") await stageKubernetes(c, data);
    else if (s.post_completed) {
      const { stageGithub } = await import("./github-artifact-DGWBDSTW.js");
      await stageGithub(c, data);
    }
    return true;
  } catch (error) {
    note(s.staging_errors, error instanceof Error ? error.message : "Staging failed");
    console.error(`guard: ${s.staging_errors.at(-1)}`);
    await persist(c, s);
    return false;
  }
}
async function githubState(c) {
  const core = await import("./core-TPCNNC45.js");
  core.saveState("guard_config", JSON.stringify(c));
}
async function main(c, deps = {}, status = newStatus(), provider = "local") {
  const proof = deps.proof ?? runProof;
  status.main_started = true;
  await saveStatus(c, status);
  let assignmentValid = false;
  try {
    await validateAssignment(c, provider);
    assignmentValid = true;
    await stage(c, status, deps);
    const begin = await proof(c, ["begin", "--assignment", c.assignment, "--sandbox-root", c.sandboxRoot], c.ctlTimeoutMs);
    status.begin = summary(begin);
    await persist(c, status);
    if (!successful(begin)) throw new Error("proofctl begin failed");
    const state = JSON.parse(begin.stdout);
    if (state.kind !== "cleanup-context/v1" || !/^[a-f0-9]{32}$/.test(state.token) || state.namespace !== `proof-${state.token}` || state.sandbox !== join(resolve(c.sandboxRoot), state.namespace)) {
      throw new Error("proofctl begin returned invalid workspace identity");
    }
    const inventory = await proof(c, ["inventory"], c.ctlTimeoutMs);
    status.inventory = summary(inventory);
    if (!successful(inventory)) throw new Error("proofctl inventory failed");
    status.main_completed = true;
    await saveStatus(c, status);
    if (!await stage(c, status, deps)) throw new Error("Cannot stage main completion");
    return { PROOF_WORKSPACE: join(state.sandbox, "workspace"), PROOF_CREDENTIALS: join(state.sandbox, "credentials"), PROOF_NAMESPACE: state.namespace };
  } catch (error) {
    note(status.errors, error instanceof Error ? error.message : "Guard main failed");
    await persist(c, status);
    if (assignmentValid) await stage(c, status, deps);
    throw error;
  }
}
function canonical(value) {
  if (value === null || typeof value !== "object") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${canonical(value[key])}`).join(",")}}`;
}
function validateEvidence(raw) {
  if (Buffer.byteLength(raw) > MAX_EVIDENCE_BYTES) throw new Error("Evidence exceeds 900 KiB");
  const parsed = JSON.parse(raw);
  if (parsed.kind !== "cleanup-evidence/v1" || canonical(parsed) !== raw) throw new Error("Receipt is not canonical cleanup evidence");
}
async function post(c, deps = {}, given) {
  const s = given ?? await loadStatus(c);
  const proof = deps.proof ?? runProof;
  s.post_started = true;
  await persist(c, s);
  await stage(c, s, deps);
  const deadline = Date.now() + c.cleanupTimeoutMs;
  try {
    for (let attempt = 0; attempt < c.cleanupAttempts; attempt++) {
      const remaining = deadline - Date.now();
      if (remaining <= 0) break;
      const result = await proof(c, ["cleanup", "--timeout", `${Math.max(1, remaining - 100)}ms`], remaining);
      s.cleanup_results.push(summary(result));
      s.cleanup_exit_code = result.exit_code;
      s.cleanup_signal = result.signal;
      s.cleanup_timed_out ||= result.timed_out;
      await persist(c, s);
      if (successful(result)) break;
    }
    if (s.cleanup_results.length === 0 || s.cleanup_results.some((r) => !successful(r))) note(s.errors, "Cleanup failed or timed out; all attempts retained");
  } catch {
    note(s.errors, "Cleanup execution failed");
  }
  let evidence;
  try {
    const result = await proof(c, ["receipt"], c.ctlTimeoutMs);
    s.receipt = summary(result);
    if (!successful(result)) throw new Error("proofctl receipt failed");
    validateEvidence(result.stdout);
    evidence = result.stdout;
    await writeFile(c.evidence, evidence, { mode: 384 });
  } catch (error) {
    note(s.errors, error instanceof Error ? error.message : "Receipt failed");
  }
  s.post_completed = true;
  await persist(c, s);
  if (!await stage(c, s, deps, evidence)) await stage(c, s, deps, evidence);
  await persist(c, s);
  console.error(`guard-status: ${JSON.stringify(s)}`);
  return s;
}
function guardFailed(s) {
  return !s.main_completed || !s.post_completed || s.errors.length > 0 || s.staging_errors.length > 0;
}

export {
  newStatus,
  note,
  saveStatus,
  loadStatus,
  githubState,
  main,
  validateEvidence,
  post,
  guardFailed
};
