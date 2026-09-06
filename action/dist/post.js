import { createRequire as __guardCreateRequire } from 'node:module'; const require = __guardCreateRequire(import.meta.url);
import {
  getState,
  setFailed
} from "./chunk-OQPMKFPK.js";
import {
  guardFailed,
  post
} from "./chunk-G6DBLZ2U.js";
import "./chunk-OKTPO7TK.js";
import {
  validateAssignment
} from "./chunk-MC4MAVAB.js";
import {
  configFromEnv,
  validateConfig
} from "./chunk-R7C4D23A.js";
import "./chunk-26V5X43U.js";
import "./chunk-3U3XFDXN.js";

// src/post.ts
process.on("SIGTERM", () => {
});
process.on("SIGINT", () => {
});
try {
  const saved = getState("guard_config");
  const config = saved ? JSON.parse(saved) : configFromEnv();
  validateConfig(config);
  await validateAssignment(config, "github");
  if (guardFailed(await post(config))) setFailed("Guard post recorded cleanup, receipt or staging failure");
} catch (error) {
  setFailed(`guard post failed: ${error instanceof Error ? error.message : "unknown error"}`);
}
