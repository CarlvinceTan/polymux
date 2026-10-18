<script lang="ts">
  import {onMount} from 'svelte';
  import logo from './assets/polymux.svg';
  import github from 'simple-icons/icons/github.svg?url';
  import DownloadIcon from './lib/DownloadIcon.svelte';
  import MobileMenu from './lib/MobileMenu.svelte';
  import {detectPlatform, PLATFORM_DETAILS, type Platform, type ReleaseDownloads} from './lib/platform';

  const githubUrl = 'https://github.com/CarlvinceTan/polymux';
  const releaseUrl = `${githubUrl}/releases/latest`;
  const platformCopy: Record<Platform, {action: string; detail: string}> = {
    macos: {action: 'Download', detail: PLATFORM_DETAILS.macos},
    windows: {action: 'Download for Windows', detail: PLATFORM_DETAILS.windows},
    linux: {action: 'Download for Linux', detail: PLATFORM_DETAILS.linux},
    other: {action: 'View desktop releases', detail: 'macOS · Windows · Linux'},
  };
  const views = [
    {name: 'Conversations', description: 'Talk to an agent, attach files, and search your messages.'},
    {name: 'Groups', description: 'Bring several agents into one conversation with a shared purpose.'},
    {name: 'Profiles', description: 'Give each agent a name, role, instructions, and an avatar.'},
  ];

  let platform = $state<Platform>('macos');
  let release = $state<ReleaseDownloads | null>(null);
  let copy = $derived(platformCopy[platform]);
  let download = $derived(platform === 'other' ? null : release?.platforms[platform] ?? null);
  let downloadUrl = $derived(download?.url ?? release?.releaseUrl ?? releaseUrl);
  let versionLabel = $derived(release?.version ? `Version ${release.version}` : 'Latest release');

  onMount(async () => {
    platform = detectPlatform();
    try {
      const response = await fetch('/api/downloads');
      if (response.ok) release = await response.json() as ReleaseDownloads;
    } catch {
      // GitHub Releases remains the fallback when release metadata is unavailable.
    }
  });
</script>

<svelte:head>
  <link rel="icon" type="image/png" sizes="32x32" href="/favicon-32.png" />
</svelte:head>

<header id="top" class="site-shell-header">
  <div class="site-shell-inner">
    <div class="site-shell-primary">
      <a class="site-shell-brand" href="#top" aria-label="Polymux home"><img src={logo} alt="" /><span>Polymux</span></a>
      <nav class="site-shell-nav" aria-label="Main navigation">
        <a class="active" href="#top">Home</a>
        <a href="/docs/">Docs</a>
        <a href="/releases/">Releases</a>
      </nav>
    </div>
    <div class="site-shell-actions">
      <a class="site-shell-github" href={githubUrl} aria-label="Polymux on GitHub">
        <img src={github} alt="" />
      </a>
      <a class="site-shell-download" href="/docs/installation/"><DownloadIcon />Install</a>
    </div>
    <MobileMenu active="home" downloadHref="/docs/installation/" />
  </div>
</header>

<main>
  <section class="hero">
    <div class="hero-copy">
      <p class="eyebrow">Your personal assistant</p>
      <h1>Your agents.<br />One conversation away.</h1>
      <p class="lede">A native app for conversations with AI agents. Connect to your agent server, work in individual or group chats, and shape how each agent helps.</p>
      <div class="actions">
        <a class="primary" href="/docs/quickstart/">Get started</a>
        <a class="secondary" href="#workspace">See how it works <span>↓</span></a>
      </div>
      <p class="availability">Native source: macOS 27 and iOS 27 · Go agent server</p>
    </div>
  </section>

  <section class="intro" id="workspace">
    <p class="eyebrow">The native app</p>
    <h2>Keep the conversation at the centre.</h2>
  </section>

  <section class="views" aria-label="Native Polymux features">
    {#each views as view (view.name)}
      <article>
        <div class="view-copy"><h3>{view.name}</h3><p>{view.description}</p></div>
      </article>
    {/each}
  </section>

  <section class="simple">
    <p class="eyebrow">Set up your way</p>
    <h2>Your app.<br />Your agent server.</h2>
    <p>The Go server runs agents, stores history and memory, and connects their tools. Configure model access on the server, then connect from the native app.</p>
  </section>

  <section class="download" id="download">
    <div><p class="eyebrow">Earlier desktop releases</p><h2>Looking for an<br />Electron download?</h2><p>These packages use the older workspace, not the native interface. <a href="/docs/installation/">Check installation options</a>.</p></div>
    <div class="download-action"><a class="primary download-cta" href={downloadUrl}><DownloadIcon />{copy.action}</a><span>{copy.detail} · {versionLabel} · Electron</span></div>
  </section>
</main>

<footer>
  <a class="brand" href="#top"><img src={logo} alt="" /><span>Polymux</span></a>
  <p>Native conversations with your agents.</p>
  <a href="/product/">Product</a>
  <a href="/docs/">Docs</a>
  <a href="/releases/">Releases</a>
  <a href="/privacy-policy/">Privacy</a>
  <a href={githubUrl}>GitHub</a>
</footer>

<style>
  .hero { padding-bottom: 0; }
  .hero-copy { margin-bottom: 0; }
  .availability { color: var(--muted); }
</style>
