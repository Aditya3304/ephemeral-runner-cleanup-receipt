import { createRequire as __guardCreateRequire } from 'node:module'; const require = __guardCreateRequire(import.meta.url);
import {
  checkPayload
} from "./chunk-26V5X43U.js";
import "./chunk-3U3XFDXN.js";

// src/github-artifact.ts
import { Worker } from "node:worker_threads";
var stageGithub = async (config, data) => {
  if (config.transport !== "github" || process.env.GITHUB_ACTIONS !== "true") throw new Error("Artifact upload requires explicit GitHub transport");
  checkPayload(data);
  await new Promise((resolve, reject) => {
    const worker = new Worker(new URL("./github-upload-worker.js", import.meta.url), { workerData: { config, data } });
    let done = false;
    const finish = async (error) => {
      if (done) return;
      done = true;
      clearTimeout(timer);
      await worker.terminate();
      if (error) reject(error);
      else resolve();
    };
    const timer = setTimeout(() => void finish(new Error("GitHub artifact upload timed out")), 35e3);
    worker.once("message", (message) => void finish(message.ok ? void 0 : new Error("GitHub artifact upload failed")));
    worker.once("error", () => void finish(new Error("GitHub artifact worker failed")));
    worker.once("exit", () => {
      if (!done) void finish(new Error("GitHub artifact worker exited without confirmation"));
    });
  });
};
export {
  stageGithub
};
