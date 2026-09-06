import * as core from '@actions/core';
import { configFromEnv } from './config.js';
import { githubState, main } from './lifecycle.js';

try {
  const config = configFromEnv();
  // Register continuity before any begin side effects, including failed main.
  await githubState(config);
  const env = await main(config, {}, undefined, 'github');
  for (const [key, value] of Object.entries(env)) { core.exportVariable(key, value); core.setOutput(key.slice(6).toLowerCase(), value); }
} catch (error) {
  core.setFailed(`guard main failed: ${error instanceof Error ? error.message : 'unknown error'}`);
}
