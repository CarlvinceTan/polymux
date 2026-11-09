import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {dirname, join} from 'node:path';
import test from 'node:test';
import {parse} from 'yaml';
import {loadPublishedReleases} from '../scripts/content.ts';

const workflow = (name) => parse(readFileSync(new URL(`../../../.github/workflows/${name}.yml`, import.meta.url), 'utf8'));
const project = readFileSync(new URL('../../Polymux/Polymux.xcodeproj/project.pbxproj', import.meta.url), 'utf8');
const versions = [...project.matchAll(/MARKETING_VERSION = ([^;]+);/g)].map((match) => match[1]);
const version = versions[0];

test('development delivery runs only on dev pushes or manual dispatch, upstream, and only when enabled', () => {
  const {on: events, jobs} = workflow('dev');
  assert.deepEqual(Object.keys(events), ['push', 'workflow_dispatch']);
  assert.deepEqual(events.push.branches, ['dev']);
  assert.equal(jobs.verify.if, "github.ref == 'refs/heads/dev' && github.repository == 'CarlvinceTan/polymux' && (github.event_name == 'workflow_dispatch' || vars.DEV_DELIVERY == 'true')");
  assert.equal(jobs.verify.uses, './.github/workflows/tests.yml');
  assert.equal(jobs.version.needs, 'verify');
  for (const name of ['ios', 'macos']) {
    assert.equal(jobs[name].needs, 'version');
    assert.equal(jobs[name].environment, 'development');
  }
});

test('the app version has published native notes matching the release tag and download allowlist', () => {
  assert.ok(version);
  assert.equal(new Set(versions).size, 1, 'Debug and Release versions differ');
  const releases = loadPublishedReleases();
  const current = releases.find((release) => release.version === version);
  assert.ok(current, `Missing published notes for ${version}`);
  assert.equal(current.tag, `app-v${version}`);
  assert.equal(current.downloadable, true);
  assert.equal(current.historical, false);
  assert.equal(releases.find((release) => release.downloadable && !release.historical)?.version, version);
});

test('the release preflight accepts prepared notes and rejects missing, draft, or mismatched releases', async (context) => {
  const script = workflow('release').jobs.macos.steps.find((step) => step.name === 'Resolve version').run;
  const notes = readFileSync(new URL(`../src/content/releases/${version}.md`, import.meta.url), 'utf8');
  const cases = [
    {name: 'prepared native release', notes, status: 0},
    {name: 'missing notes', notes: null, error: /Publish .* on the site/},
    {name: 'draft notes', notes: notes.replace('published: true', 'published: false'), error: /must be published/},
    {name: 'disabled downloads', notes: notes.replace('downloadable: true', 'downloadable: false'), error: /must be downloadable/},
    {name: 'Electron tag', notes: notes.replace(`tag: app-v${version}`, `tag: v${version}`), error: /must use tag: app-v/},
    {name: 'mismatched pushed tag', notes, ref: 'tag', tag: 'app-v99.0.0', error: /does not match MARKETING_VERSION/},
  ];
  for (const item of cases) await context.test(item.name, () => {
    const root = mkdtempSync(join(tmpdir(), 'polymux-release-'));
    const write = (path, body) => {
      const target = join(root, path);
      mkdirSync(dirname(target), {recursive: true});
      writeFileSync(target, body);
    };
    try {
      write('apps/Polymux/Polymux.xcodeproj/project.pbxproj', project);
      if (item.notes !== null) write(`apps/site/src/content/releases/${version}.md`, item.notes);
      const environment = join(root, 'github-env');
      const result = spawnSync('bash', ['-e', '-o', 'pipefail', '-c', script], {
        cwd: root, encoding: 'utf8', timeout: 10_000,
        env: {...process.env, GITHUB_ENV: environment, GITHUB_REF_TYPE: item.ref ?? 'branch', GITHUB_REF_NAME: item.tag ?? 'dev'},
      });
      assert.equal(result.status, item.status ?? 1, result.stdout + result.stderr);
      if (item.error) assert.match(result.stdout + result.stderr, item.error);
      else assert.equal(readFileSync(environment, 'utf8'), `VERSION=${version}\nTAG=app-v${version}\n`);
    } finally {rmSync(root, {recursive: true});}
  });
});
