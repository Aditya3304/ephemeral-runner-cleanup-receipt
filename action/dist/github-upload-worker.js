import { createRequire as __guardCreateRequire } from 'node:module'; const require = __guardCreateRequire(import.meta.url);
import {
  validateAssignment
} from "./chunk-MC4MAVAB.js";
import {
  checkPayload
} from "./chunk-26V5X43U.js";
import "./chunk-3U3XFDXN.js";

// src/github-upload-worker.ts
import { parentPort, workerData } from "node:worker_threads";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
var ok = false;
var directory;
try {
  const { config, data } = workerData;
  if (!parentPort || config.transport !== "github" || process.env.GITHUB_ACTIONS !== "true") throw new Error("GitHub-only worker");
  checkPayload(data);
  await validateAssignment(config, "github");
  const artifact = await import("./artifact-OHOTIGGI.js");
  await mkdir(dirname(config.status), { recursive: true });
  directory = await mkdtemp(join(dirname(config.status), "guard-upload-"));
  const files = [];
  for (const [key, value] of Object.entries(data)) {
    const file = join(directory, key);
    await writeFile(file, value, { mode: 384 });
    files.push(file);
  }
  const identity = [process.env.GITHUB_RUN_ID, process.env.GITHUB_RUN_ATTEMPT, process.env.GITHUB_JOB].join("-").replace(/[^a-zA-Z0-9_-]/g, "_").slice(0, 120);
  const result = await new artifact.DefaultArtifactClient().uploadArtifact(`cleanup-preliminary-${identity}`, files, directory, { retentionDays: 1 });
  if (!result.id) throw new Error("Artifact upload returned no ID");
  ok = true;
} catch {
  ok = false;
} finally {
  if (directory) await rm(directory, { recursive: true, force: true });
}
parentPort?.postMessage({ ok });
