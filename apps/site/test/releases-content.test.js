import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {join} from 'node:path';
import test from 'node:test';
import {loadPublishedReleases, releasesContentRoot, validateReleaseBody} from '../scripts/content.ts';
import {
  ALL_PLATFORMS,
  filterReleaseChangelog,
  groupReleaseChangelog,
  parseReleaseChangelog,
  releasePlatforms,
  resolveReleasePlatform,
} from '../lib/content/changelog.ts';

/** Read a published release body (front matter stripped) for parsing tests. */
function releaseBody(version) {
  const source = readFileSync(join(releasesContentRoot, `${version}.md`), 'utf8');
  const match = source.match(/^---\r?\n[\s\S]*?\r?\n---\r?\n([\s\S]*)$/);
  assert.ok(match, `expected ${version}.md to have YAML front matter`);
  return match[1].trim();
}

test('loads every published Polymux release in newest-first order', () => {
  const releases = loadPublishedReleases();
  const versions = releases.map(({version}) => version);
  assert.ok(['0.3.0', '0.2.4', '0.2.3', '0.2.2', '0.2.1', '0.2.0', '0.1.0'].every((version) => versions.includes(version)));
  assert.deepEqual(releases.map((release) => release.date), releases.map((release) => release.date).toSorted().reverse());
  assert.equal(new Set(releases.map(({version}) => version)).size, releases.length);
  assert.match(releases.find((release) => release.version === '0.3.0')?.summary ?? '', /release/i);
  assert.equal(releases.find(({version}) => version === '0.2.0')?.downloadable, false);
  assert.equal(releases.find(({version}) => version === '0.1.0')?.downloadable, false);
  assert.ok(releases.filter((release) => versions.indexOf(release.version) >= versions.indexOf('0.3.0')).every((release) => release.historical && !release.downloadable));
});

test('multiline Markdown bullets retain their continuation and unsupported prose fails validation', () => {
  const body = '## Features\n### Desktop\n- A change with\n  **important details**.\n';
  const sections = parseReleaseChangelog(body);
  assert.match(sections[0].items[0], /important details/);
  assert.throws(() => validateReleaseBody('probe.md', body + '\nAn ungrouped caveat.\n'), /unsupported paragraph/);
});

test('repeated category areas merge without duplicate panel keys or mutating parsed sections', () => {
  const sections = parseReleaseChangelog('## Features\n### Desktop\n- First.\n### AI\n- Second.\n### Desktop\n- Third.\n');
  const groups = groupReleaseChangelog(sections);
  assert.deepEqual(groups[0].sections.map((section) => section.area), ['Desktop', 'AI']);
  assert.deepEqual(groups[0].sections[0].items, ['First.', 'Third.']);
  assert.deepEqual(sections[0].items, ['First.']);
});

test('requires category and product-area sections for release notes', () => {
  assert.throws(
    () => validateReleaseBody('0.3.0.md', '## Features\n\n- Added something.'),
    /category and area/,
  );
  assert.doesNotThrow(() => validateReleaseBody('0.3.0.md', '## Features\n\n### Hub\n\n- Added something.'));
});

test('groups a release body into ordered platform sections', () => {
  const body = releaseBody('0.3.0');
  const sections = parseReleaseChangelog(body);

  assert.deepEqual(
    sections.map(({category, area}) => `${category}: ${area}`),
    [
      'Features: Desktop', 'Features: AI', 'Features: CLI', 'Features: Phone',
      'Features: Browser', 'Features: Hub', 'Features: Site',
      'Bug Fixes: AI', 'Bug Fixes: Desktop', 'Bug Fixes: Hub', 'Bug Fixes: Phone',
      'Improvements: Desktop', 'Improvements: CLI', 'Improvements: Browser',
      'Improvements: Hub', 'Improvements: Site',
    ],
  );

  const desktop = sections.find(({category, area}) => category === 'Features' && area === 'Desktop');
  assert.ok(desktop);
  assert.equal(desktop.items.length, 3);
  assert.match(desktop.items[0], /Connect account-linked devices/);

  assert.deepEqual(
    releasePlatforms({body}),
    ['Desktop', 'AI', 'CLI', 'Phone', 'Browser', 'Hub', 'Site'],
  );
});

test('filters a release to one platform while keeping category grouping', () => {
  const sections = parseReleaseChangelog(releaseBody('0.3.0'));
  const phone = filterReleaseChangelog(sections, 'Phone');

  assert.ok(phone.length > 0);
  assert.ok(phone.every(({area}) => area === 'Phone'));
  assert.deepEqual(
    groupReleaseChangelog(phone).map(({category}) => category),
    ['Features', 'Bug Fixes'],
  );
  assert.deepEqual(
    groupReleaseChangelog(phone).map(({sections: grouped}) => grouped.map(({area}) => area)),
    [['Phone'], ['Phone']],
  );
});

test('defaults to the All view and ignores unknown platforms', () => {
  const body = releaseBody('0.3.0');
  const platforms = releasePlatforms({body});
  const sections = parseReleaseChangelog(body);

  assert.equal(ALL_PLATFORMS, 'all');
  assert.equal(resolveReleasePlatform(platforms), ALL_PLATFORMS);
  assert.equal(resolveReleasePlatform(platforms, 'Nope'), ALL_PLATFORMS);
  assert.equal(resolveReleasePlatform(platforms, 'Desktop'), 'Desktop');
  assert.equal(filterReleaseChangelog(sections).length, sections.length);
  assert.equal(filterReleaseChangelog(sections, ALL_PLATFORMS).length, sections.length);
});

test('handles a release with a single platform', () => {
  const body = [
    '## Features',
    '',
    '### Desktop',
    '',
    '- First thing.',
    '- Second thing.',
    '',
    '## Improvements',
    '',
    '### Desktop',
    '',
    '- Third thing.',
  ].join('\n');

  const sections = parseReleaseChangelog(body);
  const platforms = releasePlatforms({body});

  assert.deepEqual(platforms, ['Desktop']);
  assert.deepEqual([ALL_PLATFORMS, ...platforms], ['all', 'Desktop']);
  assert.deepEqual(
    groupReleaseChangelog(filterReleaseChangelog(sections, 'Desktop')).map(({category}) => category),
    ['Features', 'Improvements'],
  );
  assert.equal(sections[0].items.length, 2);
  assert.equal(sections[1].items[0], 'Third thing.');
});
