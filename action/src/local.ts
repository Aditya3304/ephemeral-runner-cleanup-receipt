import { configFromEnv } from './config.js';
import { local } from './local-runner.js';

try {
  const delimiter = process.argv.indexOf('--', 2);
  if (delimiter < 0) throw new Error('Usage: local.js -- COMMAND [ARG ...]');
  process.exitCode = await local(configFromEnv(), process.argv.slice(delimiter + 1));
} catch (error) {
  console.error(`guard local failed: ${error instanceof Error ? error.message : 'unknown error'}`); process.exitCode = 1;
}
