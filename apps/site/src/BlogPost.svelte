<script lang="ts">
  import {onMount} from 'svelte';
  import logo from './assets/polymux.svg';
  import {formatBlogDate, getBlogPost, nextPosts} from './lib/blog';
  import MobileMenu from './lib/MobileMenu.svelte';

  const pathParts = location.pathname.split('/').filter(Boolean);
  const slug = decodeURIComponent(pathParts[pathParts.length - 1] ?? '');
  const post = getBlogPost(slug);
  const readNext = post ? nextPosts(post.slug) : [];

  let activeHeadingId = $state(post?.toc[0]?.id ?? '');

  function syncActiveHeading() {
    const headings = Array.from(document.querySelectorAll<HTMLElement>('.post-body h2[id], .post-body h3[id]'));
    if (!headings.length) return;
    let active = headings[0]?.id ?? '';
    for (const heading of headings) {
      if (heading.getBoundingClientRect().top <= 128) active = heading.id;
      else break;
    }
    activeHeadingId = active;
  }

  onMount(() => {
    if (!post?.toc.length) return;
    syncActiveHeading();
    window.addEventListener('scroll', syncActiveHeading, {passive: true});
    return () => window.removeEventListener('scroll', syncActiveHeading);
  });
</script>

<svelte:head>
  <link rel="icon" type="image/png" sizes="32x32" href="/favicon-32.png" />
  {#if post}
    <title>{post.title} — Polymux Blog</title>
    <meta name="description" content={post.excerpt} />
  {/if}
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

{#if post}
  <main class="post-page">
    <header class="post-title">
      <a class="back-link" href="/blog/"><span aria-hidden="true">←</span> All posts</a>
      <h1>{post.title}</h1>
      <p>{post.excerpt}</p>
      <div class="post-byline">
        <span class="post-author">{post.author}</span>
        <div class="post-meta"><time datetime={post.date}>{formatBlogDate(post.date)}</time><span>{post.readingMinutes} min read</span></div>
      </div>
    </header>

    {#if post.coverImage}<img class="post-cover" src={post.coverImage} alt="" />{/if}

    <div class="post-layout">
      <article class="post-body">{@html post.html}</article>

      {#if post.toc.length}
        <aside class="post-toc">
          <p>On this page</p>
          <nav aria-label="On this page">
            {#each post.toc as item (item.id)}
              <a class:active={activeHeadingId === item.id} class:nested={item.level === 3} href={`#${item.id}`}>{item.text}</a>
            {/each}
          </nav>
        </aside>
      {/if}
    </div>

    {#if readNext.length}
      <section class="post-next" aria-label="More from the blog">
        <p>Read next</p>
        <div>
          {#each readNext as item (item.slug)}
            <a href={`/blog/${item.slug}/`}>
              <div class="post-meta"><time datetime={item.date}>{formatBlogDate(item.date)}</time><span>{item.readingMinutes} min read</span></div>
              <h2>{item.title}</h2>
              <p>{item.excerpt}</p>
            </a>
          {/each}
        </div>
      </section>
    {/if}
  </main>
{:else}
  <main class="not-found">
    <p class="blog-eyebrow">404</p>
    <h1>That article isn’t here.</h1>
    <a href="/blog/">Back to the blog</a>
  </main>
{/if}

<footer class="blog-footer">
  <a class="blog-brand" href="/"><img src={logo} alt="" /><span>Polymux</span></a>
  <span>Personal software, thoughtfully built.</span>
  <a href="/releases/">Releases</a>
  <a href="/privacy-policy/">Privacy</a>
</footer>
