import { copyFile, mkdir } from 'node:fs/promises';
import path from 'node:path';

const root = process.cwd();

const files = [
  ['node_modules/htmx.org/dist/htmx.min.js', 'static/js/vendor/htmx.min.js'],
  ['node_modules/htmx-ext-sse/dist/sse.min.js', 'static/js/vendor/htmx-ext-sse.js'],
  ['node_modules/chart.js/dist/chart.umd.min.js', 'static/js/vendor/chart.umd.min.js'],
  ['node_modules/chartjs-adapter-date-fns/dist/chartjs-adapter-date-fns.bundle.min.js', 'static/js/vendor/chartjs-adapter-date-fns.bundle.min.js'],
  ['node_modules/@highlightjs/cdn-assets/highlight.min.js', 'static/js/vendor/highlight.min.js'],
  ['node_modules/@highlightjs/cdn-assets/styles/github-dark.min.css', 'static/css/vendor/github-dark.min.css'],
];

for (const [source, destination] of files) {
  const from = path.join(root, source);
  const to = path.join(root, destination);
  await mkdir(path.dirname(to), { recursive: true });
  await copyFile(from, to);
  console.log(`${source} -> ${destination}`);
}
