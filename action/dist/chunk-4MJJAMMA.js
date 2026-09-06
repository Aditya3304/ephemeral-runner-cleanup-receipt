import { createRequire as __guardCreateRequire } from 'node:module'; const require = __guardCreateRequire(import.meta.url);
import {
  guardFailed,
  main,
  newStatus,
  note,
  post,
  saveStatus
} from "./chunk-G6DBLZ2U.js";
import {
  killGroup
} from "./chunk-OKTPO7TK.js";

// src/local-runner.ts
import { spawn } from "node:child_process";
import { setTimeout as delay } from "node:timers/promises";
import { constants } from "node:os";
async function local(c, argv, deps = {}) {
  if (process.platform === "win32") throw new Error("Local process-group guard requires Linux");
  if (c.transport !== "kubernetes") throw new Error("Local wrapper requires kubernetes transport");
  const status = newStatus();
  let child;
  let phase = "main";
  let killTimer;
  const cancel = (signal2) => {
    if (phase === "post" || status.cancellation_signal) return;
    status.cancellation_signal = signal2;
    if (child) {
      try {
        killGroup(child.pid, signal2);
      } catch {
        note(status.errors, "Unable to forward cancellation");
      }
      killTimer = setTimeout(() => {
        try {
          killGroup(child?.pid, "SIGKILL");
        } catch {
          note(status.errors, "Unable to kill command group");
        }
      }, c.graceMs);
    }
  };
  const term = () => cancel("SIGTERM");
  const int = () => cancel("SIGINT");
  process.on("SIGTERM", term);
  process.on("SIGINT", int);
  try {
    if (!argv[0]) throw new Error("Expected command argv after --");
    const env = await main(c, deps, status);
    if (!status.cancellation_signal) {
      phase = "command";
      await new Promise((resolve) => {
        child = spawn(argv[0], argv.slice(1), { cwd: env.PROOF_WORKSPACE, env: { ...process.env, ...env }, shell: false, detached: true, stdio: "inherit" });
        const timer = setTimeout(() => {
          status.command_timed_out = true;
          cancel("SIGTERM");
        }, c.jobTimeoutMs);
        child.once("error", () => {
          clearTimeout(timer);
          note(status.errors, "Unable to spawn user command");
          resolve();
        });
        child.once("exit", (code, signal2) => {
          clearTimeout(timer);
          status.command_exit_code = code;
          status.command_signal = signal2;
          resolve();
        });
      });
    }
  } catch (error) {
    note(status.errors, error instanceof Error ? error.message : "Local guard failed");
  } finally {
    phase = "post";
    clearTimeout(killTimer);
    try {
      if (child?.pid) {
        killGroup(child.pid, "SIGTERM");
        await delay(c.graceMs);
        killGroup(child.pid, "SIGKILL");
      }
    } catch {
      note(status.errors, "Command process-group cleanup failed");
    }
    try {
      await saveStatus(c, status);
    } catch {
      note(status.errors, "Unable to persist command result");
    }
    try {
      await post(c, deps, status);
    } finally {
      process.removeListener("SIGTERM", term);
      process.removeListener("SIGINT", int);
    }
  }
  if (status.command_exit_code !== null && status.command_exit_code !== 0) return status.command_exit_code;
  if (status.command_timed_out) return 124;
  const signal = status.command_signal ?? status.cancellation_signal;
  if (signal) return 128 + (constants.signals[signal] ?? 1);
  return guardFailed(status) ? 1 : 0;
}

export {
  local
};
