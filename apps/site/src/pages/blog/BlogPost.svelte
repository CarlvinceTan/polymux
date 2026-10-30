<script lang="ts">
  import type {PostProps} from '../../../lib/content/types';
  import {formatDate} from '../../lib/format';
  import SiteHeader from '../../components/SiteHeader.svelte';
  import SiteFooter from '../../components/SiteFooter.svelte';
  import PageOutline from '../../components/PageOutline.svelte';

  let {post, related: readNext}: PostProps = $props();
</script>

<SiteHeader active="blog" />

  <main id="main" class="post-page">
    <header class="post-title">
      <a class="back-link" href="/blog/"><span aria-hidden="true">←</span> All posts</a>
      <h1>{post.title}</h1>
      <p>{post.excerpt}</p>
      <div class="post-byline">
        <span class="post-author">{post.author}</span>
        <div class="post-meta"><time datetime={post.date}>{formatDate(post.date)}</time><span>{post.readingMinutes} min read</span></div>
      </div>
    </header>

    {#if post.coverImage}<img class="post-cover" src={post.coverImage} alt="" />{/if}

    <div class="post-layout">
      <article class="post-body prose">{@html post.html}</article>

      {#if post.toc.length}
        <aside class="post-toc">
          <PageOutline items={post.toc} />
        </aside>
      {/if}
    </div>

    {#if readNext.length}
      <section class="post-next" aria-label="More from the blog">
        <p>Read next</p>
        <div>
          {#each readNext as item (item.slug)}
            <a href={`/blog/${item.slug}/`}>
              <div class="post-meta"><time datetime={item.date}>{formatDate(item.date)}</time><span>{item.readingMinutes} min read</span></div>
              <h2>{item.title}</h2>
              <p>{item.excerpt}</p>
            </a>
          {/each}
        </div>
      </section>
    {/if}
  </main>

<SiteFooter />
