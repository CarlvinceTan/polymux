<script lang="ts">
  import type {Snippet} from 'svelte';
  import logo from '../assets/app-icon.webp';
  import github from 'simple-icons/icons/github.svg?url';
  import {githubUrl, navigation, site, type Section} from '../../lib/site';
  import MobileMenu from './MobileMenu.svelte';
  import DownloadIcon from './DownloadIcon.svelte';

  let {active, tools, installHref = site.installPath}: {active?: Section; tools?: Snippet; installHref?: string} = $props();
</script>

<a class="skip-link" href="#main">Skip to content</a>
<header class="site-header" class:with-tools={!!tools}>
  <div class="header-inner">
    <a class="site-brand" href="/" aria-label="Polymux home"><img src={logo} alt="" /><span>Polymux</span></a>
    <nav class="header-nav" aria-label="Main navigation">
      {#each navigation.filter((link) => link.id !== 'home') as link (link.id)}
        <a href={link.href} aria-current={link.id === active ? 'page' : undefined}>{link.label}</a>
      {/each}
    </nav>
    {#if tools}<div class="header-tools">{@render tools()}</div>{/if}
    <div class="header-actions">
      <a class="github-link" href={githubUrl} aria-label="Polymux on GitHub"><img src={github} alt="" /></a>
      <a class="install-link" href={installHref}><DownloadIcon />Install</a>
    </div>
    <MobileMenu {active} {installHref} />
  </div>
</header>
