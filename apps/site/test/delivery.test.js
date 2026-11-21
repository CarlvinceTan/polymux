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
  for (const job of [jobs.verify, jobs['verify-push'], jobs.version]) {
    assert.match(job.if, /github\.ref == 'refs\/heads\/dev' && github\.repository == 'CarlvinceTan\/polymux'/);
  }
  assert.equal(jobs.verify.uses, './.github/workflows/tests.yml');
  assert.equal(jobs.verify.needs, 'verify-push');
  assert.equal(events.workflow_dispatch.inputs.reuse_checks.default, false);
  const push = jobs['verify-push'];
  assert.equal(push.permissions.actions, 'read');
  assert.match(push.steps.at(-1).run, /--tests-if-present[\s\S]+--tests-only/);
  assert.deepEqual(jobs.verified.needs, ['verify', 'verify-push']);
  assert.equal(jobs.verified.if, "!cancelled() && (needs.verify.result == 'success' || (needs.verify-push.result == 'success' && needs.verify-push.outputs.missing != 'true'))");
});

test('Mac compilation can overlap tests but signing and uploads cannot', () => {
  const {jobs, permissions} = workflow('dev');
  assert.equal(jobs.version.needs, undefined);
  const stage = jobs['macos-stage'];
  assert.equal(stage.needs, 'version');
  assert.equal(stage.environment, undefined);
  assert.equal(permissions.contents, 'read');
  assert.equal(stage.permissions, undefined);
  assert.ok(!JSON.stringify(stage).includes('secrets.'));
  assert.ok(stage.steps.some((step) => step.run === 'scripts/build.sh macos-stage'));
  assert.ok(!stage.steps.some((step) => /codesign|notarytool|gh release/.test(step.run ?? '')));
  assert.deepEqual(jobs.ios.needs, ['verified', 'version']);
  assert.deepEqual(jobs.macos.needs, ['verified', 'version', 'macos-stage']);
  for (const name of ['ios', 'macos']) {
    assert.equal(jobs[name].environment, 'development');
    assert.doesNotMatch(jobs[name].if ?? '', /always|failure|cancelled/);
  }
  const handoff = jobs.macos.steps.find((step) => step.uses?.startsWith('actions/download-artifact@'));
  const sign = jobs.macos.steps.find((step) => step.run === 'scripts/build.sh macos-sign');
  assert.equal(stage.outputs.artifact, '${{ steps.stage.outputs.artifact-id }}');
  assert.equal(handoff.with['artifact-ids'], '${{ needs.macos-stage.outputs.artifact }}');
  assert.ok(jobs.macos.steps.indexOf(handoff) < jobs.macos.steps.indexOf(sign));
});

test('promotion does not wait for either delivery job or rerun its component suites', () => {
  const {jobs} = workflow('branch-topology');
  const gate = jobs.validate;
  assert.equal(gate.needs, undefined);
  assert.match(gate.steps.at(-1).run, /merge-base --is-ancestor[\s\S]+python3 scripts\/promotion\.py/);
  for (const workflowName of ['tests', 'site', 'skills']) {
    const {on: events} = workflow(workflowName);
    assert.deepEqual(events.push.branches, ['main', 'dev']);
    assert.equal(events.pull_request, undefined);
  }
  const tests = workflow('tests').jobs;
  for (const name of ['swift', 'macos', 'ios']) {
    assert.equal(tests[name].needs, 'changes');
    assert.equal(tests[name].if, "needs.changes.outputs.swift == 'true'");
  }
});

test('the app version has published native notes matching the release tag and download allowlist', () => {
  assert.ok(version);
  assert.equal(new Set(versions).size, 1, 'Debug and Release versions differ');
  const releases = loadPublishedReleases();
  const current = releases.find((release) => release.version === version);
  assert.ok(current, `Missing published notes for ${version}`);
  assert.equal(current.tag, `stable-v${version}`);
  assert.equal(current.downloadable, true);
  assert.equal(current.historical, false);
  assert.equal(releases.find((release) => release.downloadable && !release.historical)?.version, version);
});

test('manually running a native release at a tag never publishes it', () => {
  const {on: events, jobs} = workflow('release');
  assert.deepEqual(events.push.tags, ['stable-v*']);
  assert.ok(Object.hasOwn(events, 'workflow_dispatch'));
  const guard = "github.event_name == 'push' && github.ref_type == 'tag'";
  assert.equal(jobs.macos.steps.find((step) => step.name === 'Publish release').if, guard);
  assert.equal(jobs.feed.if, guard);
});

test('the release preflight accepts prepared notes and rejects missing, draft, or mismatched releases', async (context) => {
  const script = workflow('release').jobs.macos.steps.find((step) => step.name === 'Resolve version').run;
  const notes = readFileSync(new URL(`../src/content/releases/${version}.md`, import.meta.url), 'utf8');
  const cases = [
    {name: 'prepared native release', notes, status: 0},
    {name: 'missing notes', notes: null, error: /Publish .* on the site/},
    {name: 'draft notes', notes: notes.replace('published: true', 'published: false'), error: /must be published/},
    {name: 'disabled downloads', notes: notes.replace('downloadable: true', 'downloadable: false'), error: /must be downloadable/},
    {name: 'Electron tag', notes: notes.replace(`tag: stable-v${version}`, `tag: v${version}`), error: /must use tag: stable-v/},
    {name: 'mismatched pushed tag', notes, ref: 'tag', tag: 'stable-v99.0.0', error: /does not match MARKETING_VERSION/},
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
      else assert.equal(readFileSync(environment, 'utf8'), `VERSION=${version}\nTAG=stable-v${version}\n`);
    } finally {rmSync(root, {recursive: true});}
  });
});
