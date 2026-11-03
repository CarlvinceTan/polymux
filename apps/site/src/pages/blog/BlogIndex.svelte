<script lang="ts">
  import type {BlogProps} from '../../../lib/content/types';
  import {formatDate} from '../../lib/format';
  import SiteHeader from '../../components/SiteHeader.svelte';
  import SiteFooter from '../../components/SiteFooter.svelte';

  let {posts}: BlogProps = $props();
  const featured = $derived(posts[0]);
  const remaining = $derived(posts.slice(1));
</script>

<SiteHeader active="blog" />

<main id="main" class="blog-index">
  <section class="blog-intro">
    <p class="blog-eyebrow">Polymux Blog</p>
    <h1>Notes on personal software.</h1>
    <p>Product thinking, comparisons, and what we learn while building Polymux.</p>
  </section>

  {#if featured}
    <section class="post-list" aria-label="Blog posts">
      <a class="featured-post" href={`/blog/${featured.slug}/`}>
        <div class="post-meta"><time datetime={featured.date}>{formatDate(featured.date)}</time><span>{featured.readingMinutes} min read</span></div>
        <article>
          {#if featured.coverImage}<img src={featured.coverImage} alt="" />{/if}
          <h2>{featured.title}</h2>
          <p>{featured.excerpt}</p>
          <span class="read-more">Read article <span aria-hidden="true">→</span></span>
        </article>
      </a>

      {#if remaining.length}
        <div class="post-rows">
          {#each remaining as post (post.slug)}
            <a href={`/blog/${post.slug}/`}>
              <div class="post-meta"><time datetime={post.date}>{formatDate(post.date)}</time><span>{post.readingMinutes} min read</span></div>
              <article>
                <h2>{post.title}</h2>
                <p>{post.excerpt}</p>
              </article>
              <span aria-hidden="true">→</span>
            </a>
          {/each}
        </div>
      {/if}
    </section>
  {:else}
    <div class="blog-empty">The first article is on its way.</div>
  {/if}
</main>

<SiteFooter />
