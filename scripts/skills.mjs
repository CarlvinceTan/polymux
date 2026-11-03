import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { appendFileSync, mkdtempSync, readFileSync, readdirSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, isAbsolute, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

export function folderHash(root) {
  const files = [];
  function collect(folder) {
    for (const entry of readdirSync(folder, { withFileTypes: true })) {
      const path = join(folder, entry.name);
      if (entry.isDirectory() && !['.git', 'node_modules'].includes(entry.name)) collect(path);
      else if (entry.isFile()) files.push(path);
    }
  }
  collect(root);
  files.sort((a, b) => relative(root, a).localeCompare(relative(root, b)));
  const hash = createHash('sha256');
  for (const path of files) hash.update(relative(root, path).split('\\').join('/')).update(readFileSync(path));
  return hash.digest('hex');
}

function checkout(entry, folders, destination) {
  const git = (...args) => execFileSync('git', args, {
    cwd: destination,
    encoding: 'utf8',
    stdio: 'pipe',
    timeout: 60_000,
    env: { ...process.env, GIT_TERMINAL_PROMPT: '0' },
  });
  git('init', '--quiet');
  git('remote', 'add', 'origin', `https://github.com/${entry.source}.git`);
  git('fetch', '--quiet', '--depth=1', '--filter=blob:none', 'origin', entry.ref || 'HEAD');
  git('sparse-checkout', 'set', '--cone', '--', ...folders);
  git('-c', 'core.autocrlf=false', 'checkout', '--quiet', '--detach', 'FETCH_HEAD');
}

export function checkSkills(lock, fetch = checkout) {
  if (lock.version !== 1 || !lock.skills || typeof lock.skills !== 'object' || Array.isArray(lock.skills)) {
    throw new Error('Unsupported skills-lock.json format');
  }
  const result = { current: [], outdated: [], errors: [] };
  const groups = new Map();
  for (const [name, entry] of Object.entries(lock.skills)) {
    if (entry?.sourceType !== 'github' || !/^[\w.-]+\/[\w.-]+$/.test(entry.source)
        || typeof entry.skillPath !== 'string' || isAbsolute(entry.skillPath)
        || entry.skillPath.split(/[\\/]/).includes('..') || !entry.skillPath.endsWith('SKILL.md')
        || !/^[a-f0-9]{64}$/.test(entry.computedHash)) {
      result.errors.push(`${name}: unsupported source or incomplete lock entry`);
      continue;
    }
    const key = JSON.stringify([entry.source, entry.ref]);
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push([name, entry]);
  }
  for (const entries of groups.values()) {
    const destination = mkdtempSync(join(tmpdir(), 'polymux-skills-'));
    try {
      const folders = [...new Set(entries.map(([, entry]) => dirname(entry.skillPath)))];
      fetch(entries[0][1], folders, destination);
      for (const [name, entry] of entries) {
        try {
          const hash = folderHash(join(destination, dirname(entry.skillPath)));
          result[hash === entry.computedHash ? 'current' : 'outdated'].push(name);
        } catch {
          result.errors.push(`${name}: could not read its upstream skill folder`);
        }
      }
    } catch {
      const entry = entries[0][1];
      result.errors.push(`${entries.map(([name]) => name).join(', ')}: could not fetch ${entry.source}@${entry.ref || 'HEAD'}`);
    } finally {
      rmSync(destination, { recursive: true, force: true });
    }
  }
  return result;
}

function warning(message) {
  const escaped = message.replaceAll('%', '%25').replaceAll('\r', '%0D').replaceAll('\n', '%0A');
  console.log(`::warning title=Skills freshness::${escaped}`);
}

export function report(result) {
  const messages = [
    ...result.outdated.map(name => `${name}: update available`),
    ...result.errors,
  ];
  for (const message of messages) warning(message);
  const summary = `${result.current.length} current; ${result.outdated.length} update(s) available; ${result.errors.length} check error(s).`;
  console.log(summary);
  if (process.env.GITHUB_STEP_SUMMARY) {
    appendFileSync(process.env.GITHUB_STEP_SUMMARY,
      `### Skills up to date\n\nAdvisory only: updates and check errors do not fail CI.\n\n${summary}\n\n`
      + messages.map(message => `- ${message}\n`).join(''));
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    report(checkSkills(JSON.parse(readFileSync(new URL('../skills-lock.json', import.meta.url), 'utf8'))));
  } catch (error) {
    warning(`Skills freshness check could not complete: ${error.message}`);
  }
}
