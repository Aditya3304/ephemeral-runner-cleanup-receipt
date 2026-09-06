import { configFromEnv, validateConfig, type Config } from './config.js';
import { guardFailed, post } from './lifecycle.js';
import * as core from '@actions/core';
import { validateAssignment } from './assignment.js';

// Repeated cancellation must not abort a cleanup attempt or its receipt.
process.on('SIGTERM', () => {});
process.on('SIGINT', () => {});
try {
  const saved = core.getState('guard_config');
  const config: Config = saved ? JSON.parse(saved) as Config : configFromEnv();
  validateConfig(config);
  await validateAssignment(config, 'github');
  if (guardFailed(await post(config))) core.setFailed('Guard post recorded cleanup, receipt or staging failure');
} catch (error) {
  core.setFailed(`guard post failed: ${error instanceof Error ? error.message : 'unknown error'}`);
}
