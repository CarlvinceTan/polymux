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
  const installs = [
    {name: 'Server', detail: 'On the Mac where your agents live.', description: 'Runs your agents, remembers conversations, and connects your models and tools.', command: './scripts/install.sh server', anchor: 'server'},
    {name: 'Client', detail: 'On each Mac you want to chat from.', description: 'The native macOS app. Pair it with your server and pick up the conversation.', command: './scripts/install.sh app', anchor: 'client'},
  ];
  const phrases: {text: string; agent: Agent['id']; colors: string}[] = [
    {text: 'On your devices.', agent: 'whale', colors: '#8fd0ff, #a98bff 50%, #ff8ad8'},
    {text: 'Planning your day.', agent: 'atlas', colors: '#8fd0ff, #3c97ff'},
    {text: 'Learning your ways.', agent: 'nova', colors: '#d2b2ff, #ff7ad9'},
    {text: 'Checking your work.', agent: 'fern', colors: '#8ff5c0, #19c6b8'},
    {text: 'Using your tools.', agent: 'fox', colors: '#ffd27a, #ff7a59'},
  ];
  let phrase = $state(0);
  let downloads = $state.raw<ReleaseDownloads | null>(null);
  const client = $derived(downloads?.platforms.macos ?? null);
  let nightly = $state.raw<{version: string; macos: {url: string; size: number}} | null>(null);

  onMount(() => {
    if (!window.matchMedia || matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    const interval = setInterval(() => phrase = (phrase + 1) % phrases.length, 3800);
    return () => clearInterval(interval);
  });

  onMount(async () => {
    try {
      const response = await fetch('/api/downloads');
      if (response.ok) downloads = await response.json() as ReleaseDownloads;
    } catch {}
    try {
      const response = await fetch('/api/nightly');
      if (response.ok) nightly = await response.json();
    } catch {}
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
      <p class="eyebrow">Install Polymux</p>
      <h2 id="install-title">Start with <span class="glow">your Mac.</span></h2>
      <p>{client ? 'Download the macOS client, then install the server from source.' : 'Both parts are currently for macOS. Native downloads aren’t published yet; install from source using the setup guide.'}</p>
    </div>
    {#if client}
      <p class="download-line">
        <a class="primary" href={client.url}><DownloadIcon />Download Polymux {downloads?.version} for macOS</a>
        <span>{formatFileSize(client.size)}</span>
      </p>
    {/if}
    <div class="install-grid">
      {#each installs as install, index (install.name)}
        <article style:--tint={index ? agent('nova').tint : agent('atlas').tint}>
          <div class="install-label"><span>0{index + 1}</span><h3>{install.name}</h3><span class="platform">macOS</span></div>
          <h4>{install.detail}</h4>
          <p>{install.description}</p>
          <code>{install.command}</code>
          <a class="text-link" href={`/docs/installation/#${install.anchor}`}>{install.name} setup <span aria-hidden="true">↗</span></a>
        </article>
      {/each}
    </div>
    <p class="install-note">Commands run from a source checkout. <a href="/docs/installation/#requirements">Requirements</a>: macOS 27, Go 1.27.1; Xcode 27 and signing for the client.</p>
    {#if nightly}
      <p class="together">Want the latest? <a href={nightly.macos.url}>Download Nightly {nightly.version} <span aria-hidden="true">↓</span></a> · unstable, rebuilt from <code>dev</code> · {formatFileSize(nightly.macos.size)}</p>
    {/if}
    <p class="together">Using one Mac? <a href="/docs/installation/#one-mac">Install both together <span aria-hidden="true">↗</span></a></p>
  </section>
</main>

<SiteFooter />
