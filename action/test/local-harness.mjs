import { local } from '../dist/local-runner.js';
import { readFile, writeFile } from 'node:fs/promises';
const c = JSON.parse(await readFile(process.argv[2], 'utf8'));
const stage = async (_, data) => { await writeFile(`${c.status}.staged`, JSON.stringify(data)); };
process.exitCode = await local(c, process.argv.slice(3), { stage });
