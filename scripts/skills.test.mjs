import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { checkSkills, folderHash, report } from './skills.mjs';

const content = 'Skill instructions\n';
const hash = createHash('sha256').update('SKILL.md').update(content).digest('hex');
const entry = (ref) => ({ source: 'example/skills', sourceType: 'github', skillPath: 'skills/alpha/SKILL.md', computedHash: hash, ref });

test('compares complete folders with the skill lock hash format', () => {
  const root = mkdtempSync(join(tmpdir(), 'polymux-skills-test-'));
  try {
    writeFileSync(join(root, 'SKILL.md'), content);
    assert.equal(folderHash(root), hash);
    mkdirSync(join(root, 'references'));
    writeFileSync(join(root, 'references', 'guide.md'), 'Guide\n');
    assert.notEqual(folderHash(root), hash);
    const withGuide = folderHash(root);
    mkdirSync(join(root, 'node_modules'));
    writeFileSync(join(root, 'node_modules', 'ignored'), 'Ignored');
    assert.equal(folderHash(root), withGuide);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('groups fetches by source and ref, detects updates, and removes temporary files', () => {
  const lock = { version: 1, skills: { current: entry(), outdated: entry(), pinned: entry('v1') } };
  lock.skills.outdated.computedHash = '0'.repeat(64);
  const before = JSON.stringify(lock);
  const calls = [];
  const result = checkSkills(lock, (source, folders, destination) => {
    calls.push({ ref: source.ref, destination });
    assert.deepEqual(folders, ['skills/alpha']);
    mkdirSync(join(destination, 'skills/alpha'), { recursive: true });
    writeFileSync(join(destination, 'skills/alpha/SKILL.md'), content);
  });
  assert.deepEqual(result, { current: ['current', 'pinned'], outdated: ['outdated'], errors: [] });
  assert.deepEqual(calls.map(call => call.ref), [undefined, 'v1']);
  assert.ok(calls.every(call => !existsSync(call.destination)));
  assert.equal(JSON.stringify(lock), before);
});

test('reports fetch failures and unsupported entries instead of claiming they are current', () => {
  const result = checkSkills({ version: 1, skills: { failed: entry(), unsupported: { sourceType: 'local' } } }, () => {
    throw new Error('Network unavailable');
  });
  assert.deepEqual(result.current, []);
  assert.deepEqual(result.outdated, []);
  assert.equal(result.errors.length, 2);
  assert.match(result.errors.join('\n'), /Network|could not fetch/);
  assert.throws(() => checkSkills({ version: 2, skills: {} }), /Unsupported/);
});

test('writes warning annotations and a summary without setting a failing exit code', () => {
  const root = mkdtempSync(join(tmpdir(), 'polymux-skills-report-'));
  const previous = process.env.GITHUB_STEP_SUMMARY;
  const log = console.log;
  const lines = [];
  try {
    process.env.GITHUB_STEP_SUMMARY = join(root, 'summary.md');
    console.log = line => lines.push(line);
    report({ current: [], outdated: ['alpha'], errors: ['source unavailable'] });
    assert.equal(lines.filter(line => line.startsWith('::warning ')).length, 2);
    assert.match(readFileSync(process.env.GITHUB_STEP_SUMMARY, 'utf8'), /alpha: update available/);
    assert.ok(!process.exitCode);
  } finally {
    console.log = log;
    if (previous === undefined) delete process.env.GITHUB_STEP_SUMMARY;
    else process.env.GITHUB_STEP_SUMMARY = previous;
    rmSync(root, { recursive: true, force: true });
  }
});
