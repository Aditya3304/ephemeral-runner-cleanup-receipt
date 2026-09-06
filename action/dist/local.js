import { createRequire as __guardCreateRequire } from 'node:module'; const require = __guardCreateRequire(import.meta.url);
import {
  local
} from "./chunk-4MJJAMMA.js";
import "./chunk-G6DBLZ2U.js";
import "./chunk-OKTPO7TK.js";
import "./chunk-MC4MAVAB.js";
import {
  configFromEnv
} from "./chunk-R7C4D23A.js";
import "./chunk-26V5X43U.js";
import "./chunk-3U3XFDXN.js";

// src/local.ts
try {
  const delimiter = process.argv.indexOf("--", 2);
  if (delimiter < 0) throw new Error("Usage: local.js -- COMMAND [ARG ...]");
  process.exitCode = await local(configFromEnv(), process.argv.slice(delimiter + 1));
} catch (error) {
  console.error(`guard local failed: ${error instanceof Error ? error.message : "unknown error"}`);
  process.exitCode = 1;
}
