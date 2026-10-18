<script lang="ts">
  import logo from './assets/polymux.svg';
  import {blogPosts, formatBlogDate} from './lib/blog';
  import MobileMenu from './lib/MobileMenu.svelte';

  const featured = blogPosts[0];
  const remaining = blogPosts.slice(1);
</script>

<svelte:head>
  <link rel="icon" type="image/png" sizes="32x32" href="/favicon-32.png" />
</svelte:head>

<header class="blog-header">
  <div class="blog-header-inner">
    <a class="blog-brand" href="/" aria-label="Polymux home"><img src={logo} alt="" /><span>Polymux</span></a>
    <nav aria-label="Main navigation">
      <a href="/">Home</a>
      <a href="/docs/">Docs</a>
      <a href="/releases/">Releases</a>
    </nav>
    <a class="blog-download" href="/#download">Download</a>
    <MobileMenu />
  </div>
</header>

<main class="blog-index">
  <section class="blog-intro">
    <p class="blog-eyebrow">Polymux Blog</p>
    <h1>Notes on personal software.</h1>
    <p>Product thinking, comparisons, and what we learn while building Polymux.</p>
  </section>

  {#if featured}
    <section class="post-list" aria-label="Blog posts">
      <a class="featured-post" href={`/blog/${featured.slug}/`}>
        <div class="post-meta"><time datetime={featured.date}>{formatBlogDate(featured.date)}</time><span>{featured.readingMinutes} min read</span></div>
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
              <div class="post-meta"><time datetime={post.date}>{formatBlogDate(post.date)}</time><span>{post.readingMinutes} min read</span></div>
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

<footer class="blog-footer">
  <a class="blog-brand" href="/"><img src={logo} alt="" /><span>Polymux</span></a>
  <span>Personal software, thoughtfully built.</span>
  <a href="/releases/">Releases</a>
  <a href="/privacy-policy/">Privacy</a>
</footer>
