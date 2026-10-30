<script lang="ts">
  import {onMount} from 'svelte';
  import screenshot from '../../assets/polymux-macos.webp';
  import DownloadIcon from '../../components/DownloadIcon.svelte';
  import AgentDemo from '../../components/AgentDemo.svelte';
  import SiteHeader from '../../components/SiteHeader.svelte';
  import SiteFooter from '../../components/SiteFooter.svelte';
  import {formatFileSize, type ReleaseDownloads} from '../../lib/platform';

  const providers = ['OpenAI', 'Anthropic', 'Google Gemini', 'xAI', 'OpenRouter', 'Ollama'];
  const installs = [
    {name: 'Server', detail: 'On the Mac where your agents live.', description: 'Runs your agents, remembers conversations, and connects your models and tools.', command: './scripts/install.sh server', anchor: 'server'},
    {name: 'Client', detail: 'On each Mac you want to chat from.', description: 'The native macOS app. Pair it with your server and pick up the conversation.', command: './scripts/install.sh app', anchor: 'client'},
  ];
  let downloads = $state.raw<ReleaseDownloads | null>(null);
  const client = $derived(downloads?.platforms.macos ?? null);

  onMount(async () => {
    try {
      const response = await fetch('/api/downloads');
      if (response.ok) downloads = await response.json() as ReleaseDownloads;
    } catch {}
  });
</script>

<SiteHeader active="home" installHref="#download" />

<main id="main">
  <section class="hero" aria-labelledby="hero-title">
    <div class="hero-copy">
      <p class="eyebrow">Personal agents. Your setup.</p>
      <h1 id="hero-title">Your agents.<br />On your devices.</h1>
      <p class="lede">A planning partner. A fresh pair of eyes. An agent that remembers how you like things. All running on your own Mac.</p>
      <div class="actions">
        {#if client}
          <a class="primary" href={client.url}><DownloadIcon />Download for macOS</a>
        {:else}
          <a class="primary" href="#download"><DownloadIcon />Install</a>
        {/if}
        <a class="text-link" href="#explore">Explore <span aria-hidden="true">↓</span></a>
      </div>
      <p class="availability">Client + server · macOS only · Open source</p>
    </div>
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
      <h2 id="setup-title">One Mac runs the agents.<br />Your other Macs connect.</h2>
      <p>The server runs your agents and keeps their conversations and memory. Pair a client on your other Macs, or run both parts on the same Mac.</p>
    </div>
    <div class="choices">
      <div>
        <h3>Your models.</h3>
        <p>Use a cloud provider or run models locally. Add your own API keys, sign in where supported, or connect a compatible endpoint.</p>
        <ul class="providers" aria-label="Example model providers">
          {#each providers as provider (provider)}<li>{provider}</li>{/each}
          <li class="more">and more</li>
        </ul>
      </div>
      <div>
        <h3>Your tools.</h3>
        <p>Choose the browser and devices your agents can use. Connect tools through MCP and CLIs, and add skills for the way you work.</p>
        <a class="text-link" href="/docs/skills-and-mcp/">Tool setup <span aria-hidden="true">↗</span></a>
      </div>
    </div>
  </section>

  <section class="installation" id="download" aria-labelledby="install-title">
    <div class="section-heading">
      <p class="eyebrow">Install Polymux</p>
      <h2 id="install-title">Start with your Mac.</h2>
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
        <article>
          <div class="install-label"><span>0{index + 1}</span><h3>{install.name}</h3><span class="platform">macOS</span></div>
          <h4>{install.detail}</h4>
          <p>{install.description}</p>
          <code>{install.command}</code>
          <a class="text-link" href={`/docs/installation/#${install.anchor}`}>{install.name} setup <span aria-hidden="true">↗</span></a>
        </article>
      {/each}
    </div>
    <p class="install-note">Commands run from a source checkout. <a href="/docs/installation/#requirements">Requirements</a>: macOS 27, Go 1.27.1; Xcode 27 and signing for the client.</p>
    <p class="together">Using one Mac? <a href="/docs/installation/#one-mac">Install both together <span aria-hidden="true">↗</span></a></p>
  </section>
</main>

<SiteFooter />
