import { build } from 'esbuild';
import { mkdir, readdir, unlink, writeFile } from 'node:fs/promises';
import { basename, dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const actionDirectory = dirname(fileURLToPath(import.meta.url));
if (resolve('.') !== actionDirectory) throw new Error('Run the build from action/');
await mkdir('dist', { recursive: true });
// ESM splitting keeps the artifact client dormant and out of the local startup
// graph; all runtime chunks remain under dist, with no external npm imports.
const result = await build({
  entryPoints: ['main', 'post', 'local', 'github-upload-worker', 'lifecycle', 'local-runner', 'config', 'staging', 'process', 'assignment'].map(name => `src/${name}.ts`),
  outdir: 'dist', bundle: true, splitting: true, format: 'esm', platform: 'node',
  target: 'node24', sourcemap: false, metafile: true, legalComments: 'linked',
  // debug's optional terminal-color probe is absent from the toolkit lockfile.
  // Bundle an explicit no-color result instead of retaining an external require.
  plugins: [{ name: 'optional-terminal-color', setup(builder) {
    builder.onResolve({ filter: /^supports-color$/ }, () => ({ path: 'supports-color', namespace: 'guard-optional' }));
    builder.onLoad({ filter: /.*/, namespace: 'guard-optional' }, () => ({ contents: 'module.exports = false;', loader: 'js' }));
  } }],
  banner: { js: "import { createRequire as __guardCreateRequire } from 'node:module'; const require = __guardCreateRequire(import.meta.url);" },
});
for (const output of Object.values(result.metafile.outputs)) {
  for (const item of output.imports) {
    if (item.external && !item.path.startsWith('node:') && !process.getBuiltinModule(item.path)) throw new Error(`Unbundled runtime import: ${item.path}`);
  }
}
await writeFile('dist/package.json', '{"type":"module"}\n');
await writeFile('work/bundle-meta.json', JSON.stringify(result.metafile));
// Drop only obsolete generated files directly in the verified action/dist path.
// A failed build never prunes the previously successful output.
const current = new Set([...Object.keys(result.metafile.outputs).map(path => basename(path)), 'package.json']);
const distDirectory = join(actionDirectory, 'dist');
for (const entry of await readdir(distDirectory, { withFileTypes: true })) {
  if (entry.isFile() && /^[\w-]+\.js(?:\.LEGAL\.txt)?$/.test(entry.name) && !current.has(entry.name)) {
    await unlink(join(distDirectory, entry.name));
  }
}
