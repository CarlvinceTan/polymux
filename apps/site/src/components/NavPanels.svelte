<script lang="ts">
  import menu from '../../.generated/menu.json';
  import {agent, type Agent} from '../lib/agents';
  import {formatDate} from '../lib/format';
  import type {Section} from '../../lib/site';

  let {openId, links, height}: {openId: Section | null; links: readonly {id: Section; href: string}[]; height: number} = $props();

  const guideAgents: Agent['id'][] = ['atlas', 'penguin', 'nova', 'whale', 'fox'];
  const versionTints = ['#9157ff', '#1786ff', '#00c46e'];
  let guide = $state(0);
  const order = $derived(links.map((link) => link.id));
  const current = $derived(openId ? order.indexOf(openId) : -1);
  const preview = $derived(agent(guideAgents[guide % guideAgents.length]));
</script>

{#snippet viewAll(href: string, label: string)}
  <a class="panel-all" href={href} tabindex={-1}>{label} <span aria-hidden="true">↗</span></a>
{/snippet}

<div class="nav-panels" id="nav-panels" style:height={openId ? `${height}px` : '0px'}>
  {#each links as link, index (link.id)}
    <div
      class="nav-panel"
      class:active={openId === link.id}
      inert={openId !== link.id}
      data-panel={link.id}
      style:--shift={current < 0 ? 0 : index - current}
    >
      {#if link.id === 'docs'}
        <div class="docs-panel">
          <ul>
            {#each menu.docs as page, index (page.href)}
              <li>
                <a href={page.href} class:selected={guide === index} onpointerenter={() => guide = index} onfocus={() => guide = index}>
                  <strong>{page.title}</strong><span>{page.description}</span>
                </a>
              </li>
            {/each}
          </ul>
          <a class="guide-card" href={menu.docs[guide]?.href} tabindex={-1} style:--tint={preview.tint}>
            {#key guide}<img src={preview.src} alt="" width="96" height="96" />{/key}
            <span class="guide-index">{String(guide + 1).padStart(2, '0')} / {String(menu.docs.length).padStart(2, '0')}</span>
            <strong>{menu.docs[guide]?.title}</strong>
            <span class="guide-go" aria-hidden="true">↗</span>
          </a>
        </div>
      {:else if link.id === 'blog'}
        <div class="list-panel">
          <header><span>Latest posts</span>{@render viewAll(link.href, 'All posts')}</header>
          <ul>
            {#each menu.posts as post (post.href)}
              <li><a href={post.href}><i style:--tint="#ff3aa0"><img src={agent('heart').src} alt="" width="40" height="40" /></i><span><strong>{post.title}</strong><small>{formatDate(post.date)} · {post.readingMinutes} min read</small></span></a></li>
            {/each}
          </ul>
        </div>
      {:else}
        <div class="list-panel">
          <header><span>Latest releases</span>{@render viewAll(link.href, 'All releases')}</header>
          <ul>
            {#each menu.releases as release, index (release.href)}
              <li><a href={release.href}><i class="version" style:--tint={versionTints[index % versionTints.length]}>{release.version}</i><span><strong>{release.title}</strong><small>{formatDate(release.date)}</small></span></a></li>
            {/each}
          </ul>
        </div>
      {/if}
    </div>
  {/each}
</div>
