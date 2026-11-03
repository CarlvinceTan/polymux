<script lang="ts">
  import github from 'simple-icons/icons/github.svg?url';
  import DownloadIcon from './DownloadIcon.svelte';
  import {githubUrl, navigation, site, type Section} from '../../lib/site';

  let {active, installHref = site.installPath}: {active?: Section; installHref?: string} = $props();
  let open = $state(false);
  let menuRoot: HTMLDivElement;
  let toggle: HTMLButtonElement;

  function closeMenu(restore = false) {
    open = false;
    if (restore) toggle.focus();
  }
</script>

<svelte:window
  onclick={(event) => { if (open && !event.composedPath().includes(menuRoot)) closeMenu(); }}
  onkeydown={(event) => { if (open && event.key === 'Escape') closeMenu(true); }}
  onresize={() => { if (window.innerWidth > 1050 && open) closeMenu(); }}
/>

<div class="mobile-menu" class:open bind:this={menuRoot}>
  <button
    class="mobile-menu-toggle"
    type="button"
    aria-label={open ? 'Close navigation menu' : 'Open navigation menu'}
    aria-expanded={open}
    aria-controls="mobile-navigation-drawer"
    bind:this={toggle}
    onclick={() => open = !open}
  >
    <span></span><span></span><span></span>
  </button>

  <div id="mobile-navigation-drawer" class="mobile-menu-drawer" aria-hidden={!open} inert={!open}>
    <div class="mobile-menu-links" role="navigation" aria-label="Mobile navigation">
      {#each navigation as link (link.id)}
        <a
          class:current={link.id === active}
          href={link.href}
          aria-current={link.id === active ? 'page' : undefined}
          onclick={() => closeMenu()}
        >{link.label}</a>
      {/each}
    </div>
    <div class="mobile-menu-actions">
      <a class="mobile-menu-download" href={installHref} onclick={() => closeMenu()}>
        <DownloadIcon />
        <span>Install</span>
      </a>
      <a class="mobile-menu-github" href={githubUrl} onclick={() => closeMenu()}>
        <img src={github} alt="" />
        <span>GitHub</span>
      </a>
    </div>
  </div>
</div>
