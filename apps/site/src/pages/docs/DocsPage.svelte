<script lang="ts">
  import type {DocsProps} from '../../../lib/content/types';
  import {docsPath} from '../../../lib/site';
  import SiteHeader from '../../components/SiteHeader.svelte';
  import SiteFooter from '../../components/SiteFooter.svelte';
  import PageOutline from '../../components/PageOutline.svelte';

  let {page, sections, pages, neighbours}: DocsProps = $props();
  let searchQuery = $state('');
  let searchInput: HTMLInputElement;
  let menuButton: HTMLButtonElement;
  let drawer: HTMLDialogElement;
  let closedSections = $state(new Set<string>());
  const searchResults = $derived.by(() => {
    const query = searchQuery.trim().toLowerCase();
    return query.length < 2 ? [] : pages.filter((candidate) =>
      `${candidate.title} ${candidate.description} ${candidate.section} ${candidate.text}`.toLowerCase().includes(query),
    ).slice(0, 7);
  });

  function toggleSection(section: string) {
    const next = new Set(closedSections);
    if (next.has(section)) next.delete(section);
    else next.add(section);
    closedSections = next;
  }

  function handleKey(event: KeyboardEvent) {
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
      event.preventDefault();
      searchInput.focus();
    }
    if (event.key === 'Escape') searchQuery = '';
  }
</script>

<svelte:window onkeydown={handleKey} />

{#snippet navigation()}
  <nav aria-label="Documentation">
    {#each sections as section (section.title)}
      <section>
        <button type="button" aria-expanded={!closedSections.has(section.title)} onclick={() => toggleSection(section.title)}>
          <span>{section.title}</span><span aria-hidden="true">{closedSections.has(section.title) ? '+' : '−'}</span>
        </button>
        {#if !closedSections.has(section.title)}
          <ul>{#each section.pages as item (item.slug)}
            <li><a class:active={page.slug === item.slug} aria-current={page.slug === item.slug ? 'page' : undefined} href={docsPath(item.slug)}>{item.title}</a></li>
          {/each}</ul>
        {/if}
      </section>
    {/each}
  </nav>
{/snippet}

{#snippet tools()}
  <button class="docs-menu-button" type="button" aria-label="Open documentation navigation" aria-haspopup="dialog" aria-controls="docs-navigation" bind:this={menuButton} onclick={() => drawer.showModal()}>Guides</button>
  <div class="docs-search-wrap">
    <label class="docs-search" for="docs-search">
      <span class="sr-only">Search documentation</span>
      <svg viewBox="0 0 20 20" aria-hidden="true"><circle cx="8.5" cy="8.5" r="5.5" /><path d="m13 13 4 4" /></svg>
      <input bind:this={searchInput} bind:value={searchQuery} id="docs-search" type="search" placeholder="Search documentation" autocomplete="off" />
      <kbd>⌘K</kbd>
    </label>
    {#if searchQuery.trim().length >= 2}
      <div class="docs-search-results">
        {#if searchResults.length}
          <nav aria-label="Search results">{#each searchResults as result (result.slug)}
            <a href={docsPath(result.slug)}><strong>{result.title}</strong><small>{result.description}</small></a>
          {/each}</nav>
        {:else}<p role="status">No matching documentation.</p>{/if}
      </div>
    {/if}
  </div>
{/snippet}

<SiteHeader active="docs" {tools} />
<dialog id="docs-navigation" class="docs-drawer" aria-label="Documentation navigation" bind:this={drawer} onclose={() => menuButton.focus({preventScroll: true})}>
  <header><strong>Documentation</strong><button type="button" onclick={() => drawer.close()}>Close</button></header>
  {@render navigation()}
</dialog>

<div class="docs-layout">
  <aside class="docs-sidebar">{@render navigation()}</aside>
  <main id="main" class="docs-main">
    <article class="docs-article prose">{@html page.html}</article>
    {#if neighbours.previous || neighbours.next}
      <nav class="docs-neighbours" aria-label="Previous and next documentation pages">
        {#if neighbours.previous}<a href={docsPath(neighbours.previous.slug)}><small>← Previous</small><strong>{neighbours.previous.title}</strong></a>{:else}<span></span>{/if}
        {#if neighbours.next}<a class="next" href={docsPath(neighbours.next.slug)}><small>Next →</small><strong>{neighbours.next.title}</strong></a>{/if}
      </nav>
    {/if}
  </main>
  {#if page.toc.length}<aside class="docs-toc"><PageOutline items={page.toc} /></aside>{/if}
</div>
<SiteFooter />
