// SPDX-License-Identifier: BSD-3-Clause

import { build, context } from "esbuild";
import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { basename, join } from "node:path";

const serve = process.argv.includes("--serve");
const outdir = serve ? "node_modules/.cache/console" : "../pkg/webconsole/static";
const template = await readFile("index.html", "utf8");

const options = {
  entryPoints: ["src/main.tsx"],
  bundle: true,
  format: "esm",
  target: "es2022",
  minify: !serve,
  outdir,
  entryNames: serve ? "assets/[name]" : "assets/[name]-[hash]",
  define: { "process.env.NODE_ENV": JSON.stringify(serve ? "development" : "production") },
};

if (serve) {
  await mkdir(outdir, { recursive: true });
  await writeFile(join(outdir, "index.html"), template);
  const compiler = await context(options);
  await compiler.watch();
  const { port } = await compiler.serve({ host: "127.0.0.1", port: 5173, servedir: outdir });
  console.log("http://127.0.0.1:" + port);
} else {
  const result = await build({ ...options, metafile: true, write: false });
  await rm(join(outdir, "assets"), { recursive: true, force: true });
  await mkdir(join(outdir, "assets"), { recursive: true });
  for (const file of result.outputFiles) await writeFile(file.path, file.contents);
  const [js, entry] = Object.entries(result.metafile.outputs).find(([, output]) => output.entryPoint);
  const html = template
    .replace("./assets/main.js", "./assets/" + basename(js))
    .replace("./assets/main.css", "./assets/" + basename(entry.cssBundle));
  await writeFile(join(outdir, "index.html"), html);
}
