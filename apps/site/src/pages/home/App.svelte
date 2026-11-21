<script lang="ts">
  import {onMount} from 'svelte';
  import screenshot from '../../assets/polymux-macos.webp';
  import DownloadIcon from '../../components/DownloadIcon.svelte';
  import AgentDemo from '../../components/AgentDemo.svelte';
  import AgentWorld from '../../components/AgentWorld.svelte';
  import {agent, type Agent} from '../../lib/agents';
  import SiteHeader from '../../components/SiteHeader.svelte';
  import SiteFooter from '../../components/SiteFooter.svelte';
  import {formatFileSize, type ReleaseDownloads} from '../../lib/platform';

  const providers = ['OpenAI', 'Anthropic', 'Google Gemini', 'xAI', 'OpenRouter', 'Ollama'];
  const installCommand = 'curl -fsSL https://polymux.com/install-cli.sh | sh';
  const installTabs = ['Desktop', 'Phone', 'Terminal'] as const;
  const betaLink = import.meta.env.VITE_TESTFLIGHT_URL?.trim() ?? '';
  const phoneDownload = /^https:\/\/testflight\.apple\.com\/join\/[a-z\d]+$/i.test(betaLink) ? betaLink : '';
  const phrases: {text: string; agent: Agent['id']; colors: string}[] = [
    {text: 'On your devices.', agent: 'whale', colors: '#8fd0ff, #a98bff 50%, #ff8ad8'},
    {text: 'Planning your day.', agent: 'atlas', colors: '#8fd0ff, #3c97ff'},
    {text: 'Learning your ways.', agent: 'nova', colors: '#d2b2ff, #ff7ad9'},
    {text: 'Checking your work.', agent: 'fern', colors: '#8ff5c0, #19c6b8'},
    {text: 'Using your tools.', agent: 'fox', colors: '#ffd27a, #ff7a59'},
  ];
  let phrase = $state(0);
  let downloads = $state.raw<ReleaseDownloads | null>(null);
  let nightly = $state.raw<{version: string | null; build: string; macos: {name: string; url: string; size: number}} | null>(null);
  let resolved = $state(false);
  let copyStatus = $state('');
  let selectedTab = $state<(typeof installTabs)[number]>('Desktop');
  const client = $derived(resolved ? nightly?.macos ?? downloads?.platforms.macos ?? null : null);
  const version = $derived(nightly ? nightly.version : downloads?.version);
  const archive = $derived(client?.name.endsWith('.zip') ?? false);

  function selectTab(event: KeyboardEvent, index: number) {
    const last = installTabs.length - 1;
    const next = event.key === 'ArrowRight' ? (index + 1) % installTabs.length
      : event.key === 'ArrowLeft' ? (index + last) % installTabs.length
      : event.key === 'Home' ? 0 : event.key === 'End' ? last : -1;
    if (next < 0) return;
    event.preventDefault();
    selectedTab = installTabs[next];
    (event.currentTarget as HTMLElement).parentElement?.querySelectorAll<HTMLButtonElement>('[role=tab]')[next]?.focus();
  }

  async function copyInstall() {
    try {
      await navigator.clipboard.writeText(installCommand);
      copyStatus = 'Copied';
    } catch {
      copyStatus = 'Select and copy the command.';
    }
  }

  onMount(() => {
    if (!window.matchMedia || matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    const interval = setInterval(() => phrase = (phrase + 1) % phrases.length, 3800);
    return () => clearInterval(interval);
  });

  onMount(() => {
    const controller = new AbortController();
    const result = (path: string) => fetch(path, {signal: controller.signal})
      .then((response) => response.ok ? response.json() : null).catch(() => null);
    void Promise.all([result('/api/nightly'), result('/api/downloads')]).then(([preview, stable]) => {
      if (controller.signal.aborted) return;
      nightly = preview;
      downloads = stable;
      resolved = true;
    });
    return () => controller.abort();
  });
</script>

<SiteHeader active="home" installHref="#download" />

<main id="main">
  <section class="hero" aria-labelledby="hero-title">
    <div class="aurora" aria-hidden="true"><i></i><i></i><i></i><i></i><i></i></div>
    <div class="hero-copy">
      <h1 id="hero-title">
        Your agents.<br />
        <span class="sr-only">On your devices.</span>
        <span class="phrases" aria-hidden="true">
          {#each phrases as item, index (item.text)}
            <span class:shown={phrase === index} class:gone={phrase !== index} style:--colors={item.colors}>{item.text}</span>
          {/each}
        </span>
      </h1>
      <p class="lede">A planning partner. A fresh pair of eyes. An agent that remembers how you like things. All running on your own Mac.</p>
      <div class="actions">
        {#if client}
          <a class="primary" href={client.url}><DownloadIcon />Download for macOS</a>
        {:else}
          <a class="primary" href="#download"><DownloadIcon />Install</a>
        {/if}
        <a class="secondary" href="#explore">Explore <span aria-hidden="true">↓</span></a>
      </div>
      <p class="availability">Client + server · macOS only · Open source</p>
    </div>
    <AgentWorld active={phrases[phrase].agent} />
    <AgentDemo />
    <details class="app-preview">
      <summary>App preview <span aria-hidden="true">+</span></summary>
      <figure>
        <a href={screenshot} aria-label="View the macOS app screenshot at full size"><img src={screenshot} width="2360" height="1544" loading="lazy" alt="Polymux for macOS, showing three agents and a conversation with Atlas planning the day." /></a>
        <figcaption>The native macOS app. A real conversation in the current development build. Select the image for full size.</figcaption>
      </figure>
    </details>
  </section>

  <section class="setup" id="how-it-works" aria-labelledby="setup-title">
    <div class="section-heading">
      <p class="eyebrow">Two parts. One conversation.</p>
      <h2 id="setup-title">One Mac runs the agents.<br /><span class="glow">Your other Macs connect.</span></h2>
      <p>The server runs your agents and keeps their conversations and memory. Pair a client on your other Macs, or run both parts on the same Mac.</p>
    </div>
    <div class="choices">
      <div style:--tint={agent('whale').tint}>
        <i class="chip"><img src={agent('whale').src} alt="" width="40" height="40" loading="lazy" /></i>
        <h3>Your models.</h3>
        <p>Use a cloud provider or run models locally. Add your own API keys, sign in where supported, or connect a compatible endpoint.</p>
        <ul class="providers" aria-label="Example model providers">
          {#each providers as provider (provider)}<li>{provider}</li>{/each}
          <li class="more">and more</li>
        </ul>
      </div>
      <div style:--tint={agent('fox').tint}>
        <i class="chip"><img src={agent('fox').src} alt="" width="40" height="40" loading="lazy" /></i>
        <h3>Your tools.</h3>
        <p>Choose the browser and devices your agents can use. Connect tools through MCP and CLIs, and add skills for the way you work.</p>
        <a class="text-link" href="/docs/skills-and-mcp/">Tool setup <span aria-hidden="true">↗</span></a>
      </div>
    </div>
  </section>

  <section class="installation" id="download" aria-labelledby="install-title">
    <div class="section-heading">
      <h2 id="install-title">Install <span class="glow">Polymux.</span></h2>
    </div>
    <div class="install-tabs" role="tablist" aria-label="Installation options">
      {#each installTabs as tab, index (tab)}
        <button type="button" role="tab" id={`install-tab-${tab}`} aria-controls={`install-panel-${tab}`}
          aria-selected={selectedTab === tab} tabindex={selectedTab === tab ? 0 : -1}
          onclick={() => selectedTab = tab} onkeydown={(event) => selectTab(event, index)}>{tab}</button>
      {/each}
    </div>
    <div class="install-panel" role="tabpanel" id="install-panel-Desktop" aria-labelledby="install-tab-Desktop" tabindex="0" hidden={selectedTab !== 'Desktop'}>
      <p>{archive ? 'Unzip the download, then drag Polymux to Applications and open it to start setup.' : 'Open the disk image, drag Polymux to Applications, then open it to start setup.'} The Mac app includes the server.</p>
      {#if client}
        <div class="download-line">
          <a class="primary" href={client.url}><DownloadIcon />Download for macOS</a>
          <p>{version ? `Version ${version}` : `Build ${nightly?.build}`} · {nightly ? 'Nightly · ' : ''}{archive ? 'ZIP' : 'DMG'} · {formatFileSize(client.size)}</p>
        </div>
        <p class="install-note">Apple silicon · macOS 27 or later</p>
      {:else}
        <p class="install-note"><a href="/docs/installation/">Installation guide</a></p>
      {/if}
    </div>
    <div class="install-panel" role="tabpanel" id="install-panel-Phone" aria-labelledby="install-tab-Phone" tabindex="0" hidden={selectedTab !== 'Phone'}>
      <p>Install the iPhone beta, then pair it with your Polymux server.</p>
      {#if phoneDownload}
        <a class="primary" href={phoneDownload}><DownloadIcon />Install TestFlight beta</a>
      {:else}
        <button class="primary" type="button" disabled>Install TestFlight beta</button>
        <p class="install-note">The public beta isn’t open yet.</p>
      {/if}
    </div>
    <div class="install-panel" role="tabpanel" id="install-panel-Terminal" aria-labelledby="install-tab-Terminal" tabindex="0" hidden={selectedTab !== 'Terminal'}>
      <p>Install the stable CLI on Apple silicon{downloads?.platforms.macos ? '' : ' when a stable release is available'}.</p>
      <div class="terminal-command">
        <code>{installCommand}</code>
        <button type="button" onclick={copyInstall} aria-label="Copy Terminal install command">Copy</button>
      </div>
      <p class="terminal-status" role="status">{copyStatus}</p>
      <p class="install-note">Run <code>polymux app download</code> to get the stable Mac disk image.</p>
    </div>
  </section>
</main>

<SiteFooter />
