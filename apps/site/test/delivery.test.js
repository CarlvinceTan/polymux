import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {dirname, join} from 'node:path';
import test from 'node:test';
import {parse} from 'yaml';

const workflow = (name) => parse(readFileSync(new URL(`../../../.github/workflows/${name}.yml`, import.meta.url), 'utf8'));
const config = JSON.parse(readFileSync(new URL('../../tauri/src-tauri/tauri.conf.json', import.meta.url), 'utf8'));
const version = config.version;

test('the Tauri app has a stable release version', () => {
  assert.match(version, /^\d+\.\d+\.\d+$/);
});

test('the release workflow builds, notarizes and uploads the Tauri app', () => {
  const steps = workflow('release').jobs.macos.steps;
  assert.ok(steps.some((step) => step.run?.includes('npm run tauri')));
  assert.ok(steps.some((step) => step.run?.includes('notarytool')));
  assert.ok(steps.some((step) => step.uses?.startsWith('actions/upload-artifact')));
});

test('the release preflight matches the tag to the Tauri version', async (context) => {
  const script = workflow('release').jobs.macos.steps.find((step) => step.name === 'Resolve version').run;
  const cases = [
    {name: 'branch build', ref: 'branch', tag: 'dev', status: 0},
    {name: 'matching tag', ref: 'tag', tag: `app-v${version}`, status: 0},
    {name: 'mismatched tag', ref: 'tag', tag: 'app-v99.0.0', error: /does not match the Tauri version/},
  ];
  for (const item of cases) await context.test(item.name, () => {
    const root = mkdtempSync(join(tmpdir(), 'polymux-release-'));
    const write = (path, body) => {
      const target = join(root, path);
      mkdirSync(dirname(target), {recursive: true});
      writeFileSync(target, body);
    };
    try {
      write('apps/tauri/src-tauri/tauri.conf.json', JSON.stringify({version}));
      const environment = join(root, 'github-env');
      const result = spawnSync('bash', ['-e', '-o', 'pipefail', '-c', script], {
        cwd: root, encoding: 'utf8', timeout: 10_000,
        env: {...process.env, GITHUB_ENV: environment, GITHUB_REF_TYPE: item.ref, GITHUB_REF_NAME: item.tag},
      });
      assert.equal(result.status, item.status ?? 1, result.stdout + result.stderr);
      if (item.error) assert.match(result.stdout + result.stderr, item.error);
      else assert.equal(readFileSync(environment, 'utf8'), `VERSION=${version}\nTAG=app-v${version}\n`);
    } finally {rmSync(root, {recursive: true});}
  });
});
