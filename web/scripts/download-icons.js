import { writeFileSync, mkdirSync, rmSync, existsSync, readdirSync, readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
import { extract } from 'tar';

const SCRIPT_DIR = dirname(fileURLToPath(import.meta.url));
const WEB_DIR = join(SCRIPT_DIR, '..');
const ICONS_DIR = join(WEB_DIR, 'public', 'icons');
const CACHE_PATH = join(SCRIPT_DIR, '.download-cache.json');
const MANIFEST_PATH = join(SCRIPT_DIR, 'icons.json');

const manifest = JSON.parse(readFileSync(MANIFEST_PATH, 'utf-8'));

function loadCache() {
  try {
    return JSON.parse(readFileSync(CACHE_PATH, 'utf-8'));
  } catch {
    return {};
  }
}

function saveCache(cache) {
  writeFileSync(CACHE_PATH, JSON.stringify(cache, null, 2));
}

function listIconFiles(dest) {
  return readdirSync(dest)
    .filter((file) => file.endsWith('.svg'))
    .sort();
}

function hashExtractedIcons(dest, files) {
  const hash = createHash('sha256');

  for (const file of files) {
    hash.update(file);
    hash.update('\0');
    hash.update(readFileSync(join(dest, file)));
    hash.update('\0');
  }

  return hash.digest('hex');
}

async function downloadAndVerify(name, config, cache) {
  const dest = join(ICONS_DIR, name);

  if (cache[name]?.sha256 === config.sha256 && cache[name]?.count && cache[name]?.contentHash && existsSync(dest)) {
    const files = listIconFiles(dest);
    if (files.length === cache[name].count) {
      const contentHash = hashExtractedIcons(dest, files);
      if (contentHash === cache[name].contentHash) {
        console.log(`✓ ${name}/ (${files.length} icons, cached, sha256 verified)`);
        return;
      }
    }
  }

  const ref = config.version;
  let url;
  if (config.refType === 'commits') {
    url = `https://github.com/${config.repo}/archive/${ref}.tar.gz`;
  } else {
    url = `https://github.com/${config.repo}/archive/refs/${config.refType}/${ref}.tar.gz`;
  }

  console.log(`Downloading ${name}/ (${config.repo} @ ${ref}) ...`);
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(`Failed to download ${name}: HTTP ${res.status} ${res.statusText}`);
  }

  const buffer = Buffer.from(await res.arrayBuffer());

  const hash = createHash('sha256').update(buffer).digest('hex');
  if (hash !== config.sha256) {
    throw new Error(
      `${name}: SHA256 mismatch!\n  Expected: ${config.sha256}\n  Actual:   ${hash}\n  Update the version and sha256 in web/scripts/icons.json.`
    );
  }

  rmSync(dest, { recursive: true, force: true });
  mkdirSync(dest, { recursive: true });

  const tmpDir = join(WEB_DIR, '.tmp-icons');
  mkdirSync(tmpDir, { recursive: true });
  const archivePath = join(tmpDir, `${name}.tar.gz`);
  writeFileSync(archivePath, buffer);

  const archivePrefix = `${config.repo.split('/')[1]}-${ref.replace(/^v/, '')}`;

  await extract({
    file: archivePath,
    cwd: dest,
    strip: 2,
    filter: (path) => {
      return path.startsWith(`${archivePrefix}/${config.svgDir}/`) && path.endsWith('.svg');
    },
  });

  rmSync(tmpDir, { recursive: true, force: true });

  const files = listIconFiles(dest);
  const contentHash = hashExtractedIcons(dest, files);
  console.log(`  → ${files.length} icons saved to ${name}/ (sha256 verified)`);

  cache[name] = { sha256: config.sha256, count: files.length, contentHash };
}

async function main() {
  const clean = process.argv.includes('--clean');
  if (clean) {
    console.log('Cleaning existing icons...');
    for (const name of Object.keys(manifest)) {
      rmSync(join(ICONS_DIR, name), { recursive: true, force: true });
    }
    saveCache({});
  }

  mkdirSync(ICONS_DIR, { recursive: true });
  const cache = clean ? {} : loadCache();

  for (const [name, config] of Object.entries(manifest)) {
    await downloadAndVerify(name, config, cache);
  }

  saveCache(cache);
  console.log('Done.');
}

main().catch((err) => {
  console.error(err.message);
  process.exit(1);
});
