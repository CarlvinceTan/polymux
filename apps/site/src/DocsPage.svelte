<script lang="ts">
  import {onMount} from 'svelte';
  import github from 'simple-icons/icons/github.svg?url';
  import logo from './assets/polymux.svg';
  import {docsNeighbours, docsPages, docsPath, docsSections, getDocsPage} from './lib/docs';
  import MobileMenu from './lib/MobileMenu.svelte';

  const parts = location.pathname.split('/').filter(Boolean);
  const slug = parts[0] === 'docs' && parts[1] ? decodeURIComponent(parts[1]) : 'introduction';
  const page = getDocsPage(slug);
  const neighbours = page ? docsNeighbours(page.slug) : {};

  let mobileNavigationOpen = $state(false);
  let searchQuery = $state('');
  let searchInput: HTMLInputElement;
  let activeHeadingId = $state(page?.toc[0]?.id ?? '');
  let closedSections = $state(new Set<string>());

  const searchResults = $derived.by(() => {
    const query = searchQuery.trim().toLowerCase();
    if (query.length < 2) return [];
    return docsPages.filter((candidate) =>
      `${candidate.title} ${candidate.description} ${candidate.section} ${candidate.body}`.toLowerCase().includes(query),
    ).slice(0, 7);
  });

  function toggleSection(section: string) {
    const next = new Set(closedSections);
    if (next.has(section)) next.delete(section);
    else next.add(section);
    closedSections = next;
  }

  function syncActiveHeading() {
    const headings = Array.from(document.querySelectorAll<HTMLElement>('.docs-article h2[id], .docs-article h3[id]'));
    if (!headings.length) return;
    let active = headings[0]?.id ?? '';
    for (const heading of headings) {
      if (heading.getBoundingClientRect().top <= 112) active = heading.id;
      else break;
    }
    activeHeadingId = active;
  }

  onMount(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault();
        searchInput?.focus();
      }
      if (event.key === 'Escape') {
        searchQuery = '';
        mobileNavigationOpen = false;
      }
    };
    window.addEventListener('scroll', syncActiveHeading, {passive: true});
    document.addEventListener('keydown', onKeyDown);
    syncActiveHeading();
    return () => {
      window.removeEventListener('scroll', syncActiveHeading);
      document.removeEventListener('keydown', onKeyDown);
    };
  });
</script>

<svelte:head>
  <link rel="icon" type="image/png" sizes="32x32" href="/favicon-32.png" />
  {#if page}
    <title>{page.title} — Polymux Docs</title>
    <meta name="description" content={page.description} />
  {/if}
</svelte:head>

<header class="site-shell-header">
  <div class="site-shell-inner docs-site-shell-inner">
    <div class="site-shell-primary">
      <a class="site-shell-brand docs-brand" href="/" aria-label="Polymux home">
        <img src={logo} alt="" /><strong>Polymux</strong>
      </a>
      <button class="docs-menu-button" type="button" aria-label={mobileNavigationOpen ? 'Close documentation navigation' : 'Open documentation navigation'} aria-expanded={mobileNavigationOpen} onclick={() => mobileNavigationOpen = !mobileNavigationOpen}>
        {#if mobileNavigationOpen}
          <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M4 4l12 12M16 4L4 16" /></svg>
        {:else}
          <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3 5h14M3 10h14M3 15h14" /></svg>
        {/if}
      </button>
    </div>

    <div class="docs-search-wrap">
      <label class="docs-search" for="docs-search">
        <svg viewBox="0 0 20 20" aria-hidden="true"><circle cx="8.5" cy="8.5" r="5.5" /><path d="m13 13 4 4" /></svg>
        <input bind:this={searchInput} bind:value={searchQuery} id="docs-search" type="search" placeholder="Search documentation" autocomplete="off" />
        <kbd>⌘K</kbd>
      </label>
      {#if searchQuery.trim().length >= 2}
        <div class="docs-search-results">
          {#if searchResults.length}
            {#each searchResults as result (result.slug)}
              <a href={docsPath(result.slug)}>
                <span><strong>{result.title}</strong><small>{result.description}</small></span>
                <em>{result.section}</em>
              </a>
            {/each}
          {:else}
            <p>No matching documentation.</p>
          {/if}
        </div>
      {/if}
    </div>

    <div class="site-shell-actions docs-header-actions">
      <a class="site-shell-github" href="https://github.com/CarlvinceTan/polymux" aria-label="Polymux on GitHub">
        <img src={github} alt="" />
      </a>
      <a class="site-shell-download" href="/#download">Download</a>
      <MobileMenu active="docs" />
    </div>
  </div>
</header>

{#if mobileNavigationOpen}
  <button class="docs-overlay" type="button" aria-label="Close documentation navigation" onclick={() => mobileNavigationOpen = false}></button>
{/if}

<div class="docs-layout">
  <aside class:open={mobileNavigationOpen} class="docs-sidebar">
    <nav aria-label="Documentation">
      {#each docsSections as section (section.title)}
        <section>
          <button type="button" aria-expanded={!closedSections.has(section.title)} onclick={() => toggleSection(section.title)}>
            <span>{section.title}</span>
            <svg class:collapsed={closedSections.has(section.title)} viewBox="0 0 20 20" aria-hidden="true"><path d="m7 5 6 5-6 5" /></svg>
          </button>
          {#if !closedSections.has(section.title)}
            <ul>
              {#each section.pages as item (item.slug)}
                <li><a class:active={page?.slug === item.slug} href={docsPath(item.slug)}>{item.title}</a></li>
              {/each}
            </ul>
          {/if}
        </section>
      {/each}
    </nav>
  </aside>

  <main class="docs-main">
    {#if page}
      <div class="docs-content">
        <article class="docs-article">{@html page.html}</article>

        {#if neighbours.previous || neighbours.next}
          <nav class="docs-neighbours" aria-label="Previous and next documentation pages">
            {#if neighbours.previous}
              <a href={docsPath(neighbours.previous.slug)}><small>← Previous</small><strong>{neighbours.previous.title}</strong></a>
            {:else}<span></span>{/if}
            {#if neighbours.next}
              <a class="next" href={docsPath(neighbours.next.slug)}><small>Next →</small><strong>{neighbours.next.title}</strong></a>
            {/if}
          </nav>
        {/if}

        <footer class="docs-footer">
          <span>© {new Date().getFullYear()} Polymux</span>
          <div><a href="/">Home</a><a href="/releases/">Releases</a><a href="/privacy-policy/">Privacy</a></div>
        </footer>
      </div>
    {:else}
      <div class="docs-not-found"><span>404</span><h1>That page isn’t in the docs.</h1><a href="/docs/">Read the introduction</a></div>
    {/if}
  </main>

  {#if page}
    <aside class="docs-toc">
      {#if page.toc.length}
        <h2>On this page</h2>
        <nav aria-label="On this page">
          {#each page.toc as item (item.id)}
            <a class:active={activeHeadingId === item.id} class:nested={item.level === 3} href={`#${item.id}`}>{item.text}</a>
          {/each}
        </nav>
      {/if}
    </aside>
  {/if}
</div>
