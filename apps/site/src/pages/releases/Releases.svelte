<script lang="ts">
  import {onMount} from 'svelte';
  import type {ReleaseProps} from '../../../lib/content/types';
  import {githubUrl, releasePath} from '../../../lib/site';
  import {formatDate, formatMonth} from '../../lib/format';
  import {formatFileSize, PLATFORM_LABELS, visiblePlatforms, type ReleaseDownloads, type SupportedPlatform} from '../../lib/platform';
  import DownloadIcon from '../../components/DownloadIcon.svelte';
  import PlatformIcon from '../../components/PlatformIcon.svelte';
  import SiteHeader from '../../components/SiteHeader.svelte';
  import SiteFooter from '../../components/SiteFooter.svelte';

  let {release, releases}: ReleaseProps = $props();
  const githubReleaseUrl = $derived(`${githubUrl}/releases/tag/${encodeURIComponent(release.tag)}`);
  let selectedPlatform = $state('all');
  let downloads = $state.raw<ReleaseDownloads | null>(null);
  const platforms = $derived(visiblePlatforms(downloads));
  const installLabels: Record<SupportedPlatform, string> = {macos: 'Apple silicon', windows: 'Intel / AMD', linux: 'x86_64 AppImage'};

  function selectTab(event: KeyboardEvent) {
    if (!['ArrowRight', 'ArrowLeft', 'ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return;
    const button = event.currentTarget as HTMLButtonElement;
    const buttons = [...button.parentElement!.querySelectorAll<HTMLButtonElement>('[role="tab"]')];
    const current = buttons.indexOf(button);
    let next = current;
    if (event.key === 'ArrowRight' || event.key === 'ArrowDown') next = (current + 1) % buttons.length;
    else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') next = (current - 1 + buttons.length) % buttons.length;
    else next = event.key === 'End' ? buttons.length - 1 : 0;
    event.preventDefault();
    selectedPlatform = release.tabs[next].value;
    buttons[next].focus();
  }

  onMount(() => {
    if (!release.downloadable || release.historical) return;
    const controller = new AbortController();
    fetch(`/api/downloads?version=${encodeURIComponent(release.version)}`, {signal: controller.signal})
      .then(async (response) => { if (response.ok) downloads = await response.json() as ReleaseDownloads; })
      .catch(() => {});
    return () => controller.abort();
  });
</script>

<SiteHeader active="releases" />
<main id="main" class="releases-page">
  <section class="releases-intro"><h1>Releases</h1><p>See what changed in each Polymux release.</p></section>
  <div class="releases-layout">
    <aside class="versions-sidebar"><p>Versions</p><nav aria-label="Release versions">
      {#each releases as item (item.version)}<a class:active={release.version === item.version} aria-current={release.version === item.version ? 'page' : undefined} href={releasePath(item.version)}>{item.version}</a>{/each}
    </nav></aside>
    <section class="release-column">
      <p class="release-month">{formatMonth(release.date)}</p>
      <article class="release-document">
        <header><strong>{release.version}</strong><time datetime={release.date}>{formatDate(release.date, 'short')}</time></header>
        {#if release.historical}
          <p class="historical-note">Historical Electron release. Its updater and installer are retired. For the current native app, use the <a href="/docs/installation/">installation guide</a>.</p>
        {:else if release.downloadable}
          <section class="release-downloads" aria-label={`Download Polymux ${release.version}`}>
            <ul class="platform-download-grid">{#each platforms as platform (platform)}
              {@const asset = downloads?.platforms[platform]}
              <li class="platform-download-card"><div class="platform-download-heading"><PlatformIcon {platform} size={19} /><strong>{PLATFORM_LABELS[platform]}</strong></div>
                <a class="platform-download-option" href={asset?.url ?? downloads?.releaseUrl ?? githubReleaseUrl} aria-label={`Download Polymux ${release.version} for ${PLATFORM_LABELS[platform]}`}><span>{installLabels[platform]}</span><span><DownloadIcon />{asset ? formatFileSize(asset.size) : 'Download'}</span></a>
              </li>
            {/each}</ul>
            <p class="download-note"><a href={downloads?.releaseUrl ?? githubReleaseUrl}>All assets and checksums on GitHub</a></p>
          </section>
        {/if}
        <div class="release-copy">
          <p class="release-summary">{release.summary}</p><h2 class="release-title">{release.title}</h2>
          <div class="release-platforms" role="tablist" aria-label="Filter release notes by platform">{#each release.tabs as tab (tab.value)}
            <button type="button" role="tab" id={tab.tabId} aria-controls={tab.panelId} aria-selected={selectedPlatform === tab.value} tabindex={selectedPlatform === tab.value ? 0 : -1} class:active={selectedPlatform === tab.value} onclick={() => selectedPlatform = tab.value} onkeydown={selectTab}>{tab.label}</button>
          {/each}</div>
          {#each release.tabs as tab (tab.value)}
            <div class="release-body release-platform-panel prose" id={tab.panelId} role="tabpanel" aria-labelledby={tab.tabId} tabindex="0" hidden={selectedPlatform !== tab.value}>
              {#each tab.groups as group (group.category)}<h2>{group.category}</h2>
                {#each group.sections as section (section.area)}<h3>{section.area}</h3><ul>{#each section.items as item, index (`${index}:${item.html}`)}<li>{@html item.html}</li>{/each}</ul>{/each}
              {/each}
            </div>
          {/each}
        </div>
      </article>
    </section>
  </div>
</main>
<SiteFooter />
