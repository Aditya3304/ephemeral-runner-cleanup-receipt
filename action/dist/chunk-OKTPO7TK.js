import { createRequire as __guardCreateRequire } from 'node:module'; const require = __guardCreateRequire(import.meta.url);

// src/process.ts
import { spawn } from "node:child_process";
import { StringDecoder } from "node:string_decoder";
function killGroup(pid, signal) {
  if (!pid) return;
  try {
    process.kill(process.platform === "win32" ? pid : -pid, signal);
  } catch (error) {
    if (error.code !== "ESRCH") throw error;
  }
}
function successful(result) {
  return result.exit_code === 0 && !result.signal && !result.timed_out && !result.error;
}
function runProof(c, args, timeoutMs) {
  return new Promise((resolve) => {
    let stdout = "";
    let bytes = 0;
    let timed_out = false;
    let error;
    let done = false;
    const decoder = new StringDecoder("utf8");
    let reapTimer;
    const child = spawn(c.ctl, [...args, "--state", c.state, "--kubeconfig", c.kubeconfig], {
      shell: false,
      detached: process.platform !== "win32",
      windowsHide: true,
      stdio: ["ignore", "pipe", "ignore"],
      // Never allow job-controlled preload flags to inject code into a helper.
      env: { ...process.env, NODE_OPTIONS: "", NODE_PATH: "" }
    });
    const finish = (exit_code, signal) => {
      if (done) return;
      done = true;
      clearTimeout(timer);
      clearTimeout(reapTimer);
      try {
        killGroup(child.pid, "SIGKILL");
      } catch {
        error ??= "Unable to kill proofctl process group";
      }
      child.stdout.destroy();
      resolve({ exit_code, signal, timed_out, ...error ? { error } : {}, stdout: stdout + decoder.end() });
    };
    const timer = setTimeout(() => {
      timed_out = true;
      try {
        killGroup(child.pid, "SIGKILL");
      } catch {
        error = "Unable to kill timed-out proofctl";
      }
      reapTimer = setTimeout(() => finish(null, "SIGKILL"), 1e3);
    }, timeoutMs);
    child.stdout.on("data", (chunk) => {
      bytes += chunk.length;
      if (bytes > 1024 * 1024) {
        error = "proofctl output exceeds 1 MiB";
        finish(null, "SIGKILL");
      } else stdout += decoder.write(chunk);
    });
    child.on("error", () => {
      error = "Unable to execute proofctl";
      finish(null, null);
    });
    child.on("exit", (code, signal) => {
      try {
        killGroup(child.pid, "SIGKILL");
      } catch {
        error = "Unable to kill proofctl descendants";
      }
    });
    child.on("close", finish);
  });
}

export {
  killGroup,
  successful,
  runProof
};
