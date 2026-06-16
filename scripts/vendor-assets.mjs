import { createHash } from 'node:crypto';
import { copyFile, mkdir, readFile, readdir, writeFile } from 'node:fs/promises';
import path from 'node:path';

const root = process.cwd();
const manifestPath = 'internal/assets/static/vendor-manifest.json';
const noticePath = 'THIRD_PARTY_NOTICES.md';
const checkOnly = process.argv.includes('--check');
const vendorDirectories = ['internal/assets/static/js/vendor', 'internal/assets/static/css/vendor'];

const packages = {
  '@highlightjs/cdn-assets': {
    allowedLicenses: ['BSD-3-Clause'],
    upstream: 'https://github.com/highlightjs/highlight.js',
  },
  'chart.js': {
    allowedLicenses: ['MIT'],
    upstream: 'https://github.com/chartjs/Chart.js',
  },
  'chartjs-adapter-date-fns': {
    allowedLicenses: ['MIT'],
    upstream: 'https://github.com/chartjs/chartjs-adapter-date-fns',
  },
  'date-fns': {
    allowedLicenses: ['MIT'],
    upstream: 'https://github.com/date-fns/date-fns',
  },
  'htmx-ext-sse': {
    allowedLicenses: ['0BSD'],
    licenseFile: 'node_modules/htmx-ext-sse/LICENSE',
    licenseFileIncludes: 'BSD Zero Clause License',
    upstream: 'https://github.com/bigskysoftware/htmx-extensions',
  },
  'htmx.org': {
    allowedLicenses: ['0BSD'],
    upstream: 'https://github.com/bigskysoftware/htmx',
  },
};

const assets = [
  {
    source: 'node_modules/htmx.org/dist/htmx.min.js',
    destination: 'internal/assets/static/js/vendor/htmx.min.js',
    packages: ['htmx.org'],
  },
  {
    source: 'node_modules/htmx-ext-sse/dist/sse.min.js',
    destination: 'internal/assets/static/js/vendor/htmx-ext-sse.js',
    packages: ['htmx-ext-sse'],
  },
  {
    source: 'node_modules/chart.js/dist/chart.umd.min.js',
    destination: 'internal/assets/static/js/vendor/chart.umd.min.js',
    packages: ['chart.js'],
  },
  {
    source: 'node_modules/chartjs-adapter-date-fns/dist/chartjs-adapter-date-fns.bundle.min.js',
    destination: 'internal/assets/static/js/vendor/chartjs-adapter-date-fns.bundle.min.js',
    packages: ['chartjs-adapter-date-fns', 'date-fns'],
  },
  {
    source: 'node_modules/@highlightjs/cdn-assets/highlight.min.js',
    destination: 'internal/assets/static/js/vendor/highlight.min.js',
    packages: ['@highlightjs/cdn-assets'],
  },
  {
    source: 'node_modules/@highlightjs/cdn-assets/styles/github-dark.min.css',
    destination: 'internal/assets/static/css/vendor/github-dark.min.css',
    packages: ['@highlightjs/cdn-assets'],
  },
];

const lockfile = JSON.parse(await readFile(path.join(root, 'package-lock.json'), 'utf8'));
const packageMetadata = new Map();

for (const name of Object.keys(packages).sort()) {
  packageMetadata.set(name, await readPackageMetadata(name));
}

const manifest = await buildManifest();
const serializedManifest = `${JSON.stringify(manifest, null, 2)}\n`;

if (checkOnly) {
  await checkVendoredAssets(serializedManifest);
} else {
  await writeVendoredAssets(serializedManifest);
}

async function buildManifest() {
  const entries = [];
  for (const asset of assets) {
    const sourceBuffer = await readFile(path.join(root, asset.source));
    entries.push({
      file: asset.destination,
      sha256: sha256(sourceBuffer),
      source: asset.source,
      packages: asset.packages.map((name) => packageMetadata.get(name)),
    });
  }
  return {
    generatedBy: 'npm run vendor',
    assets: entries,
  };
}

async function checkVendoredAssets(expectedManifest) {
  const problems = [];

  for (const asset of assets) {
    const source = await readFile(path.join(root, asset.source));
    let vendored;
    try {
      vendored = await readFile(path.join(root, asset.destination));
    } catch (error) {
      if (error.code === 'ENOENT') {
        problems.push(`${asset.destination} is missing`);
        continue;
      }
      throw error;
    }
    if (!source.equals(vendored)) {
      problems.push(`${asset.destination} does not match ${asset.source}`);
    }
  }

  let existingManifest;
  try {
    existingManifest = await readFile(path.join(root, manifestPath), 'utf8');
  } catch (error) {
    if (error.code === 'ENOENT') {
      problems.push(`${manifestPath} is missing`);
    } else {
      throw error;
    }
  }
  if (existingManifest !== undefined && existingManifest !== expectedManifest) {
    problems.push(`${manifestPath} is stale`);
  }

  await checkNotices(problems);
  await checkUnexpectedVendoredFiles(problems);

  if (problems.length > 0) {
    console.error('Vendored assets are stale or incomplete:');
    for (const problem of problems) {
      console.error(`- ${problem}`);
    }
    console.error('Run `npm run vendor` and review internal/assets/static/vendor-manifest.json plus THIRD_PARTY_NOTICES.md.');
    process.exit(1);
  }

  console.log(`Verified ${assets.length} vendored assets and ${manifestPath}`);
}

async function checkUnexpectedVendoredFiles(problems) {
  const expectedFiles = new Set(assets.map((asset) => asset.destination));

  for (const directory of vendorDirectories) {
    let files;
    try {
      files = await listFiles(directory);
    } catch (error) {
      if (error.code === 'ENOENT') {
        problems.push(`${directory} is missing`);
        continue;
      }
      throw error;
    }

    for (const file of files) {
      if (!expectedFiles.has(file)) {
        problems.push(`${file} is not configured in scripts/vendor-assets.mjs`);
      }
    }
  }
}

async function listFiles(relativeDirectory) {
  const entries = await readdir(path.join(root, relativeDirectory), { withFileTypes: true });
  const files = [];

  for (const entry of entries) {
    const relativePath = path.posix.join(relativeDirectory, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await listFiles(relativePath)));
    } else if (entry.isFile()) {
      files.push(relativePath);
    }
  }

  return files.sort();
}

async function writeVendoredAssets(serializedManifest) {
  for (const asset of assets) {
    const from = path.join(root, asset.source);
    const to = path.join(root, asset.destination);
    await mkdir(path.dirname(to), { recursive: true });
    await copyFile(from, to);
    console.log(`${asset.source} -> ${asset.destination}`);
  }
  await writeFile(path.join(root, manifestPath), serializedManifest);
  console.log(`wrote ${manifestPath}`);
}

async function readPackageMetadata(name) {
  const lockEntry = lockfile.packages?.[`node_modules/${name}`];
  if (!lockEntry) {
    throw new Error(`package-lock.json is missing node_modules/${name}`);
  }

  const packageJSON = JSON.parse(await readFile(path.join(root, 'node_modules', name, 'package.json'), 'utf8'));
  if (packageJSON.version !== lockEntry.version) {
    throw new Error(`${name} version mismatch: package-lock.json has ${lockEntry.version}, node_modules has ${packageJSON.version}`);
  }

  const declaredLicense = lockEntry.license ?? packageJSON.license;
  const expected = packages[name];
  const license = declaredLicense ?? await fallbackLicense(name, expected);
  if (!expected.allowedLicenses.includes(license)) {
    throw new Error(`${name} license ${license} is not in allowed set: ${expected.allowedLicenses.join(', ')}`);
  }

  const metadata = {
    name,
    version: lockEntry.version,
    license,
    upstream: expected.upstream,
  };
  if (!declaredLicense && expected.licenseFile) {
    metadata.licenseSource = expected.licenseFile;
  }
  return metadata;
}

async function checkNotices(problems) {
  let notices;
  try {
    notices = await readFile(path.join(root, noticePath), 'utf8');
  } catch (error) {
    if (error.code === 'ENOENT') {
      problems.push(`${noticePath} is missing`);
      return;
    }
    throw error;
  }

  for (const asset of assets) {
    if (!notices.includes(`\`${asset.destination}\``)) {
      problems.push(`${noticePath} does not mention ${asset.destination}`);
    }
  }

  for (const metadata of packageMetadata.values()) {
    if (!notices.includes(`\`${metadata.name}\``)) {
      problems.push(`${noticePath} does not mention ${metadata.name}`);
    }
    if (!notices.includes(metadata.version)) {
      problems.push(`${noticePath} does not mention ${metadata.name} ${metadata.version}`);
    }
    if (!notices.includes(metadata.license)) {
      problems.push(`${noticePath} does not mention ${metadata.name} license ${metadata.license}`);
    }
    if (!notices.includes(metadata.upstream)) {
      problems.push(`${noticePath} does not mention ${metadata.name} upstream ${metadata.upstream}`);
    }
  }
}

async function fallbackLicense(name, expected) {
  if (!expected.licenseFile) {
    throw new Error(`${name} does not declare a package license`);
  }

  const licenseText = await readFile(path.join(root, expected.licenseFile), 'utf8');
  if (!licenseText.includes(expected.licenseFileIncludes)) {
    throw new Error(`${name} license file does not contain ${expected.licenseFileIncludes}`);
  }
  return expected.allowedLicenses[0];
}

function sha256(buffer) {
  return createHash('sha256').update(buffer).digest('hex');
}
