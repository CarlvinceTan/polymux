<script lang="ts">
  import SiteHeader from '../../components/SiteHeader.svelte';
  import SiteFooter from '../../components/SiteFooter.svelte';
  import {formatDate} from '../../lib/format';
  import {pluginPath} from '../../../lib/site';
  import type {PluginEntry, PluginProps} from '../../../lib/content/types';

  let {plugin, plugins}: PluginProps = $props();
  let query = $state('');
  let type = $state('all');
  let sort = $state('recent');

  const targets: Record<string, string> = {
    'darwin-arm64': 'macOS (Apple silicon)', 'linux-amd64': 'Linux (x86-64)', 'linux-arm64': 'Linux (Arm)',
  };

  const shown = $derived.by(() => {
    const term = query.trim().toLowerCase();
    return plugins
      .filter((item) => type === 'all' || item.kind === type || item.kind === 'both')
      .filter((item) => !term || [item.title, item.description, item.author].some((field) => field.toLowerCase().includes(term)))
      .toSorted((a, b) => sort === 'name' ? a.title.localeCompare(b.title) : (b.published ?? '').localeCompare(a.published ?? ''));
  });

  const kindLabel = (kind: string) => kind === 'both' ? 'Global and Scoped' : kind === 'global' ? 'Global' : 'Scoped';
</script>

{#snippet links(item: PluginEntry)}
  <div class="plugin-links">
    {#if item.repository}<a href={item.repository}>repo</a>{/if}
    {#if item.homepage && item.homepage !== item.repository}<a href={item.homepage}>home</a>{/if}
  </div>
{/snippet}

<SiteHeader active="plugins" />

{#if plugin}
  <main id="main" class="plugins-page">
    <header class="plugins-intro">
      <h1>{plugin.title}</h1>
      <p>{plugin.description}</p>
    </header>
    <a class="plugins-back" href="/plugins/"><span aria-hidden="true">←</span> Plugins</a>

    <section class="plugins-panel" aria-labelledby="plugin-details">
      <header><h2 id="plugin-details">Plugin details</h2></header>
      <div class="plugin-details">
        <div class="plugin-tags">
          <span class="plugin-kind">{kindLabel(plugin.kind)}</span>
          {@render links(plugin)}
        </div>
        <p>Install it from the Marketplace in Settings. Polymux asks you to confirm, then pins this version on your server.</p>
        <code class="plugin-install">Settings › Plugins › Marketplace › {plugin.title}</code>
        <dl>
          <div><dt>Plugin</dt><dd>{plugin.name}</dd></div>
          <div><dt>Version</dt><dd>{plugin.version}</dd></div>
          {#if plugin.published}<div><dt>Published</dt><dd>{formatDate(plugin.published, 'short')}</dd></div>{/if}
          {#if plugin.author}<div><dt>Author</dt><dd>{plugin.author}</dd></div>{/if}
          <div><dt>Type</dt><dd>{kindLabel(plugin.kind)}</dd></div>
          <div><dt>Servers</dt><dd>{plugin.targets.map((target) => targets[target] ?? target).join(', ')}</dd></div>
          {#if plugin.source}<div><dt>Source</dt><dd><a href={plugin.source}>{plugin.source.replace('https://github.com/', '')}</a></dd></div>{/if}
        </dl>
      </div>
    </section>

    <section class="plugins-panel" aria-labelledby="plugin-security">
      <header><h2 id="plugin-security">Security note</h2></header>
      <p class="plugin-note">Plugins run on your server. Review the source before installing third-party plugins.</p>
    </section>
  </main>
{:else}
  <main id="main" class="plugins-page">
    <header class="plugins-intro">
      <h1>Plugins</h1>
      <p>Add to your Polymux server. These are the plugins listed in Settings › Plugins › Marketplace, from the same registry.</p>
    </header>

    <section class="plugins-panel" aria-labelledby="all-plugins">
      <header>
        <h2 id="all-plugins">All plugins</h2>
        <span>{shown.length} / {plugins.length}</span>
      </header>
      {#if plugins.length}
        <div class="plugins-controls">
          <input type="search" bind:value={query} placeholder="Filter plugins…" aria-label="Filter plugins by name, description or author" />
          <select bind:value={type} aria-label="Filter by plugin type">
            <option value="all">All types</option>
            <option value="global">Global</option>
            <option value="scoped">Scoped</option>
          </select>
          <select bind:value={sort} aria-label="Sort plugins">
            <option value="recent">Recently published</option>
            <option value="name">A-Z</option>
          </select>
        </div>
      {/if}
      {#if shown.length}
        <div class="plugins-grid">
          {#each shown as item (item.name)}
            <article>
              <h3><a href={pluginPath(item.name)}>{item.title}</a></h3>
              <p>{item.description}</p>
              <div class="plugin-meta">
                {#if item.author}<span>{item.author}</span>{/if}
                <span>{item.version}</span>
                {#if item.published}<span>{formatDate(item.published, 'short')}</span>{/if}
              </div>
              <div class="plugin-tags">
                <span class="plugin-kind">{kindLabel(item.kind)}</span>
                {@render links(item)}
              </div>
            </article>
          {/each}
        </div>
      {:else}
        <p class="plugins-empty">{plugins.length ? 'No results found.' : 'No plugins listed.'}</p>
      {/if}
    </section>
  </main>
{/if}

<SiteFooter />
