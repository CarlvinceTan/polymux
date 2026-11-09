<script lang="ts">
  import type {Snippet} from 'svelte';
  import logo from '../assets/app-icon.webp';
  import github from 'simple-icons/icons/github.svg?url';
  import {githubUrl, navigation, site, type Section} from '../../lib/site';
  import MobileMenu from './MobileMenu.svelte';
  import DownloadIcon from './DownloadIcon.svelte';
  import NavPanels from './NavPanels.svelte';

  let {active, tools, installHref = site.installPath}: {active?: Section; tools?: Snippet; installHref?: string} = $props();
  const links = navigation.filter((link) => link.id !== 'home');
  let openId = $state<Section | null>(null);
  let rowWidth = $state(0);
  let panel = $state({width: 0, height: 0});
  let shell = $state<HTMLDivElement>();
  let row = $state<HTMLDivElement>();
  let timer: ReturnType<typeof setTimeout> | undefined;
  // The shell's 6px padding on each side.
  const shellWidth = $derived(rowWidth ? Math.max(rowWidth, openId ? panel.width : 0) + 12 : 0);

  function show(id: Section) {
    clearTimeout(timer);
    const element = shell?.querySelector<HTMLElement>(`[data-panel="${id}"]`);
    rowWidth = row?.offsetWidth ?? 0;
    panel = {width: element?.offsetWidth ?? 0, height: element?.offsetHeight ?? 0};
    openId = id;
  }
  function hide(delay = 160) { clearTimeout(timer); timer = setTimeout(() => openId = null, delay); }
</script>

<a class="skip-link" href="#main">Skip to content</a>
{#if tools}
  <header class="site-header with-tools">
    <div class="header-inner">
      <a class="site-brand" href="/" aria-label="Polymux home"><img src={logo} alt="" /><span>Polymux</span></a>
      <div class="header-tools">{@render tools()}</div>
      <div class="header-actions">
        <a class="github-link" href={githubUrl} aria-label="Polymux on GitHub"><img src={github} alt="" /></a>
        <a class="install-link" href={installHref}><DownloadIcon />Install</a>
      </div>
      <MobileMenu {active} {installHref} />
    </div>
  </header>
{:else}
  <header class="site-header floating">
    <div
      class="nav-shell"
      bind:this={shell}
      class:open={!!openId}
      style:width={shellWidth ? `${shellWidth}px` : undefined}
      style:--row={rowWidth ? `${rowWidth}px` : undefined}
      role="presentation"
      onpointerleave={() => hide()}
      onpointerenter={() => clearTimeout(timer)}
      onfocusout={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node)) hide(0); }}
      onkeydown={(event) => {
        if (event.key !== 'Escape' || !openId) return;
        event.currentTarget.querySelector<HTMLElement>(`[data-menu="${openId}"]`)?.focus();
        openId = null;
      }}
    >
      <div class="nav-row" bind:this={row}>
        <a class="site-brand" href="/" aria-label="Polymux home" onpointerenter={() => hide()}><img src={logo} alt="" /><span>Polymux</span></a>
        <nav class="header-nav" aria-label="Main navigation">
          {#each links as link (link.id)}
            <a
              href={link.href}
              data-menu={link.id}
              class:hovered={openId === link.id}
              aria-current={link.id === active ? 'page' : undefined}
              aria-expanded={openId === link.id}
              aria-controls="nav-panels"
              onpointerenter={() => show(link.id)}
              onfocus={() => show(link.id)}
            >{link.label}<svg viewBox="0 0 10 10" aria-hidden="true"><path d="m2.5 4 2.5 2.5L7.5 4" /></svg></a>
          {/each}
        </nav>
        <div class="header-actions" role="presentation" onpointerenter={() => hide()}>
          <a class="github-link" href={githubUrl} aria-label="Polymux on GitHub"><img src={github} alt="" /></a>
          <a class="install-link" href={installHref}><DownloadIcon />Install</a>
        </div>
        <MobileMenu {active} {installHref} />
      </div>
      <NavPanels {openId} {links} height={panel.height} />
    </div>
  </header>
{/if}
