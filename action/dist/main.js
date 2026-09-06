import { createRequire as __guardCreateRequire } from 'node:module'; const require = __guardCreateRequire(import.meta.url);
import {
  exportVariable,
  setFailed,
  setOutput
} from "./chunk-OQPMKFPK.js";
import {
  githubState,
  main
} from "./chunk-G6DBLZ2U.js";
import "./chunk-OKTPO7TK.js";
import "./chunk-MC4MAVAB.js";
import {
  configFromEnv
} from "./chunk-R7C4D23A.js";
import "./chunk-26V5X43U.js";
import "./chunk-3U3XFDXN.js";

// src/main.ts
try {
  const config = configFromEnv();
  await githubState(config);
  const env = await main(config, {}, void 0, "github");
  for (const [key, value] of Object.entries(env)) {
    exportVariable(key, value);
    setOutput(key.slice(6).toLowerCase(), value);
  }
} catch (error) {
  setFailed(`guard main failed: ${error instanceof Error ? error.message : "unknown error"}`);
}
