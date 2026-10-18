<script lang="ts">
  import logo from './assets/polymux.svg';
  import DownloadIcon from './lib/DownloadIcon.svelte';
  import FeatureIcon from './lib/FeatureIcon.svelte';
  import MobileMenu from './lib/MobileMenu.svelte';
  import {agentFeatures, appFeatures, getProductFeature, productFeaturePath, type ProductFeature} from './lib/productFeatures';

  const parts = location.pathname.split('/').filter(Boolean);
  const requestedSlug = parts[0] === 'product' && parts[1] ? decodeURIComponent(parts[1]) : null;
  const feature = requestedSlug ? getProductFeature(requestedSlug) : null;
  const notFound = requestedSlug !== null && !feature;

  function groupLabel(item: ProductFeature): string {
    return item.group === 'app' ? 'App feature' : 'Agent feature';
  }

  function relatedFeatures(item: ProductFeature): ProductFeature[] {
    return (item.group === 'app' ? appFeatures : agentFeatures).filter((candidate) => candidate.slug !== item.slug).slice(0, 3);
  }
</script>

<svelte:head>
  <link rel="icon" type="image/png" sizes="32x32" href="/favicon-32.png" />
  {#if feature}
    <title>{feature.name} — Polymux</title>
    <meta name="description" content={feature.description} />
  {:else if !notFound}
    <title>Product — Polymux</title>
    <meta name="description" content="Explore native Polymux conversations, groups, profiles, and the capabilities of its Go agent server." />
  {/if}
</svelte:head>

<header class="product-header">
  <div class="product-header-inner">
    <a class="product-brand" href="/" aria-label="Polymux home"><img src={logo} alt="" /><span>Polymux</span></a>
    <nav aria-label="Main navigation">
      <a href="/">Home</a>
      <a href="/docs/">Docs</a>
      <a href="/releases/">Releases</a>
    </nav>
    <a class="product-download" href="/docs/installation/"><DownloadIcon />Install</a>
    <MobileMenu downloadHref="/docs/installation/" />
  </div>
</header>

{#if feature}
  <main class="feature-page">
    <section class="feature-hero">
      <div class="feature-hero-copy">
        <a class="feature-group-link" href="/product/"><span aria-hidden="true">←</span> {groupLabel(feature)}</a>
        <div class="feature-title-icon"><FeatureIcon name={feature.icon} /></div>
        <h1>{feature.title}</h1>
        <p>{feature.intro}</p>
        <div class="feature-actions">
          <a class="feature-primary" href="/docs/quickstart/">Get started</a>
          <a class="feature-secondary" href={feature.docsPath}>Read the guide <span aria-hidden="true">→</span></a>
        </div>
      </div>

      <div class="feature-visual feature-demo">
        <p>{feature.demoTitle}</p>
        <div>
          {#each feature.demoItems ?? [] as item (item.title)}
            <article><span>{item.label}</span><strong>{item.title}</strong><small>{item.description}</small></article>
          {/each}
        </div>
      </div>
    </section>

    <section class="feature-benefits" aria-label={`${feature.name} benefits`}>
      {#each feature.benefits as benefit, index (benefit.title)}
        <article><span>0{index + 1}</span><h2>{benefit.title}</h2><p>{benefit.description}</p></article>
      {/each}
    </section>

    <section class="feature-how">
      <div><p class="feature-eyebrow">Using Polymux</p><h2>Start with the right setup.</h2></div>
      <ol>
        {#each feature.steps as step, index (step)}
          <li><span>{index + 1}</span><p>{step}</p></li>
        {/each}
      </ol>
    </section>

    <section class="feature-example">
      <p class="feature-eyebrow">Everyday example</p>
      <h2>{feature.example.title}</h2>
      <p>{feature.example.description}</p>
    </section>

    <section class="related-features">
      <p class="feature-eyebrow">Explore more {feature.group === 'app' ? 'app' : 'agent'} features</p>
      <div>
        {#each relatedFeatures(feature) as related (related.slug)}
          <a href={productFeaturePath(related.slug)}>
            <FeatureIcon name={related.icon} />
            <span><strong>{related.name}</strong><small>{related.menuDescription}</small></span>
            <em aria-hidden="true">→</em>
          </a>
        {/each}
      </div>
    </section>
  </main>
{:else if !notFound}
  <main class="product-overview">
    <section class="product-intro">
      <p class="feature-eyebrow">Polymux product</p>
      <h1>A place to talk with your agents.</h1>
      <p>The native macOS and iOS app connects to your Go agent server. Explore conversations and agent capabilities below, then check the installation guide for build availability.</p>
    </section>

    <section class="feature-catalogue">
      <header><span>01</span><div><p class="feature-eyebrow">App features</p><h2>Everything stays visible and close.</h2></div></header>
      <div class="feature-card-grid">
        {#each appFeatures as item (item.slug)}
          <a href={productFeaturePath(item.slug)}><FeatureIcon name={item.icon} /><h3>{item.name}</h3><p>{item.menuDescription}</p><span aria-hidden="true">→</span></a>
        {/each}
      </div>
    </section>

    <section class="feature-catalogue">
      <header><span>02</span><div><p class="feature-eyebrow">Agent features</p><h2>Helpful intelligence, shaped around you.</h2></div></header>
      <div class="feature-card-grid">
        {#each agentFeatures as item (item.slug)}
          <a href={productFeaturePath(item.slug)}><FeatureIcon name={item.icon} /><h3>{item.name}</h3><p>{item.menuDescription}</p><span aria-hidden="true">→</span></a>
        {/each}
      </div>
    </section>

    <section class="product-cta"><div><p class="feature-eyebrow">Get Polymux</p><h2>Connect your app and server.</h2></div><a class="feature-primary" href="/docs/installation/">Installation guide</a></section>
  </main>
{:else}
  <main class="product-not-found"><span>404</span><h1>That feature page isn’t here.</h1><a href="/product/">Explore Polymux features</a></main>
{/if}

<footer class="product-footer">
  <a class="product-brand" href="/"><img src={logo} alt="" /><span>Polymux</span></a>
  <span>Personal software, thoughtfully built.</span>
  <a href="/docs/">Docs</a>
  <a href="/releases/">Releases</a>
  <a href="/privacy-policy/">Privacy</a>
</footer>

<style>
  .feature-demo > p { color: var(--ink); }
  .feature-demo small { color: var(--muted); }
</style>
