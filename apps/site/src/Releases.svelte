<script lang="ts">
  import {onMount} from 'svelte';
  import github from 'simple-icons/icons/github.svg?url';
  import logo from './assets/polymux.svg';
  import {
    ALL_PLATFORMS,
    filterReleaseChangelog,
    formatReleaseDate,
    formatReleaseMonth,
    getRelease,
    groupReleaseChangelog,
    releaseChangelog,
    releasePath,
    releasePlatforms,
    releases,
  } from './lib/releases';
  import {renderSafeMarkdownInline} from './lib/markdown.js';
  import {
    formatFileSize,
    PLATFORM_LABELS,
    SUPPORTED_PLATFORMS,
    type ReleaseDownloads,
    type SupportedPlatform,
  } from './lib/platform';
  import DownloadIcon from './lib/DownloadIcon.svelte';
  import MobileMenu from './lib/MobileMenu.svelte';
  import PlatformIcon from './lib/PlatformIcon.svelte';

  const parts = location.pathname.split('/').filter(Boolean);
  const requestedVersion = parts[0] === 'releases' && parts[1] ? decodeURIComponent(parts[1]) : null;
  const release = requestedVersion ? getRelease(requestedVersion) : releases[0];
  const githubReleaseUrl = release
    ? `https://github.com/CarlvinceTan/polymux/releases/tag/v${encodeURIComponent(release.version)}`
    : 'https://github.com/CarlvinceTan/polymux/releases/latest';
  const isLatest = release ? release.version === releases[0]?.version : false;
  const installLabels: Record<SupportedPlatform, string> = {
    macos: 'Apple silicon',
    windows: 'Intel / AMD',
    linux: 'x86_64 AppImage',
  };

  type ReleaseItem = {html: string};
  type ReleaseGroup = {category: string; sections: {area: string; items: ReleaseItem[]}[]};
  type ReleasePlatformTab = {value: string; label: string; tabId: string; panelId: string; groups: ReleaseGroup[]};

  function panelKey(value: string): string {
    return value.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'all';
  }

  /** Render every platform (plus All) once, so the first paint already contains the tabs. */
  function buildPlatformTabs(): ReleasePlatformTab[] {
    if (!release) return [];
    const sections = releaseChangelog(release);
    const versionKey = panelKey(release.version);

    return [ALL_PLATFORMS, ...releasePlatforms(release)].map((value) => {
      const key = `${versionKey}-${panelKey(value)}`;
      return {
        value,
        label: value === ALL_PLATFORMS ? 'All' : value,
        tabId: `release-platform-tab-${key}`,
        panelId: `release-platform-panel-${key}`,
        groups: groupReleaseChangelog(filterReleaseChangelog(sections, value)).map((group) => ({
          category: group.category,
          sections: group.sections.map((section) => ({
            area: section.area,
            items: section.items.map((item) => ({html: renderSafeMarkdownInline(item)})),
          })),
        })),
      };
    });
  }

  const platformTabs = buildPlatformTabs();
  let selectedPlatform = $state<string>(ALL_PLATFORMS);

  function selectPlatformWithKeyboard(event: KeyboardEvent) {
    const keys = ['ArrowRight', 'ArrowLeft', 'ArrowDown', 'ArrowUp', 'Home', 'End'];
    if (!keys.includes(event.key)) return;
    const button = event.currentTarget as HTMLButtonElement | null;
    const tablist = button?.parentElement;
    if (!button || !tablist) return;
    const buttons = Array.from(tablist.querySelectorAll<HTMLButtonElement>('[role="tab"]'));
    const current = buttons.indexOf(button);
    if (current === -1) return;

    event.preventDefault();
    let next = current;
    if (event.key === 'ArrowRight' || event.key === 'ArrowDown') next = (current + 1) % buttons.length;
    else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') next = (current - 1 + buttons.length) % buttons.length;
    else if (event.key === 'End') next = buttons.length - 1;
    else next = 0;

    const target = buttons[next];
    const tab = platformTabs[next];
    if (!target || !tab) return;
    selectedPlatform = tab.value;
    target.focus();
  }

  let downloads = $state<ReleaseDownloads | null>(null);

  function assetFor(item: SupportedPlatform) {
    return downloads?.platforms[item] ?? null;
  }

  function urlFor(item: SupportedPlatform): string {
    return assetFor(item)?.url ?? downloads?.releaseUrl ?? githubReleaseUrl;
  }

  function sizeFor(item: SupportedPlatform): string {
    const asset = assetFor(item);
    return asset ? formatFileSize(asset.size) : '';
  }

  onMount(async () => {
    if (!release) return;
    try {
      const response = await fetch(`/api/downloads?version=${encodeURIComponent(release.version)}`);
      if (response.ok) downloads = await response.json() as ReleaseDownloads;
    } catch {
      // GitHub Releases remains the fallback when release metadata is unavailable.
    }
  });
</script>

<svelte:head>
  <link rel="icon" type="image/png" sizes="32x32" href="/favicon-32.png" />
  {#if release}
    <title>Polymux {release.version} Release Notes</title>
    <meta name="description" content={release.summary} />
  {/if}
</svelte:head>

<header class="site-shell-header">
  <div class="site-shell-inner">
    <div class="site-shell-primary">
      <a class="site-shell-brand" href="/" aria-label="Polymux home"><img src={logo} alt="" /><span>Polymux</span></a>
      <nav class="site-shell-nav" aria-label="Main navigation">
        <a href="/">Home</a>
        <a href="/docs/">Docs</a>
        <a class="active" href="/releases/">Releases</a>
      </nav>
    </div>
    <div class="site-shell-actions">
      <a class="site-shell-github" href="https://github.com/CarlvinceTan/polymux" aria-label="Polymux on GitHub">
        <img src={github} alt="" />
      </a>
      <a class="site-shell-download" href="/#download">Download</a>
    </div>
    <MobileMenu active="releases" />
  </div>
</header>

<main class="releases-page">
  <section class="releases-intro">
    <h1>Releases</h1>
    <p>See what’s new in each Polymux update.</p>
  </section>

  <div class="releases-layout">
    <aside class="versions-sidebar">
      <p>Versions</p>
      <nav aria-label="Release versions">
        {#each releases as item (item.version)}
          <a class:active={release?.version === item.version} href={releasePath(item.version)}>{item.version}</a>
        {/each}
      </nav>
    </aside>

    {#if release}
      <section class="release-column">
        <p class="release-month">{formatReleaseMonth(release.date)}</p>
        <article class="release-document">
          <header>
            <strong>{release.version}</strong>
            <time datetime={release.date}>{formatReleaseDate(release.date)}</time>
          </header>

          {#if release.downloadable}
            <section class="release-downloads" aria-label={`Download Polymux ${release.version}`}>
              <ul class="platform-download-grid">
                {#each SUPPORTED_PLATFORMS as item (item)}
                  <li class="platform-download-card">
                    <div class="platform-download-heading">
                      <PlatformIcon platform={item} size={19} />
                      <strong>{PLATFORM_LABELS[item]}</strong>
                    </div>
                    <a class="platform-download-option" href={urlFor(item)} aria-label={`Download Polymux ${release.version} for ${PLATFORM_LABELS[item]}`}>
                      <span>{installLabels[item]}</span>
                      <span class="platform-download-action">
                        <DownloadIcon />
                        {sizeFor(item) || 'Download'}
                      </span>
                    </a>
                  </li>
                {/each}
              </ul>

              <p class="download-note">
                <a href={downloads?.releaseUrl ?? githubReleaseUrl}>
                  {isLatest ? 'All assets and checksums on GitHub' : `All ${release.version} assets on GitHub`}
                </a>
              </p>
            </section>
          {/if}

          <div class="release-copy">
            <p class="release-summary">{release.summary}</p>
            <h2 class="release-title">{release.title}</h2>

            {#if platformTabs.length > 1}
              <div class="release-platforms" role="tablist" aria-label="Filter release notes by platform">
                {#each platformTabs as tab (tab.value)}
                  <button
                    type="button"
                    role="tab"
                    id={tab.tabId}
                    aria-controls={tab.panelId}
                    aria-selected={selectedPlatform === tab.value}
                    tabindex={selectedPlatform === tab.value ? 0 : -1}
                    class:active={selectedPlatform === tab.value}
                    onclick={() => (selectedPlatform = tab.value)}
                    onkeydown={selectPlatformWithKeyboard}
                  >{tab.label}</button>
                {/each}
              </div>

              {#each platformTabs as tab (tab.value)}
                <div
                  class="release-body release-platform-panel"
                  id={tab.panelId}
                  role="tabpanel"
                  aria-labelledby={tab.tabId}
                  tabindex="0"
                  hidden={selectedPlatform !== tab.value}
                >
                  {#each tab.groups as group (group.category)}
                    <h2>{group.category}</h2>
                    {#each group.sections as section (section.area)}
                      <h3>{section.area}</h3>
                      <ul>
                        {#each section.items as item (item.html)}
                          <li>{@html item.html}</li>
                        {/each}
                      </ul>
                    {/each}
                  {/each}
                </div>
              {/each}
            {:else}
              <div class="release-body">{@html release.html}</div>
            {/if}
          </div>
        </article>
      </section>
    {:else}
      <section class="release-not-found">
        <p>Release not found</p>
        <h2>That version isn’t here.</h2>
        <a href="/releases/">View the latest release</a>
      </section>
    {/if}
  </div>
</main>

<footer class="releases-footer">
  <a class="releases-brand" href="/"><img src={logo} alt="" /><span>Polymux</span></a>
  <span>Personal software, thoughtfully built.</span>
  <a href="/docs/">Docs</a>
  <a href="/privacy-policy/">Privacy</a>
</footer>
