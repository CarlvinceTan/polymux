<script lang="ts">
  import FeatureIcon from '../../components/FeatureIcon.svelte';
  import SiteHeader from '../../components/SiteHeader.svelte';
  import SiteFooter from '../../components/SiteFooter.svelte';
  import {productPath as productFeaturePath} from '../../../lib/site';
  import type {ProductFeature, ProductProps} from '../../../lib/content/types';

  let {feature, features}: ProductProps = $props();
  const appFeatures = $derived(features.filter((item) => item.group === 'app'));
  const agentFeatures = $derived(features.filter((item) => item.group === 'agent'));

  function groupLabel(item: ProductFeature): string {
    return item.group === 'app' ? 'App feature' : 'Agent feature';
  }

  function relatedFeatures(item: ProductFeature): ProductFeature[] {
    return (item.group === 'app' ? appFeatures : agentFeatures).filter((candidate) => candidate.slug !== item.slug).slice(0, 3);
  }
</script>

<SiteHeader active="product" />

{#if feature}
  <main id="main" class="feature-page">
    <section class="feature-hero">
      <div class="feature-hero-copy">
        <a class="feature-group-link" href="/product/"><span aria-hidden="true">←</span> {groupLabel(feature)}</a>
        <div class="feature-title-icon"><FeatureIcon name={feature.icon} /></div>
        <h1>{feature.title}</h1>
        <p>{feature.intro}</p>
        <div class="feature-actions">
          <a class="feature-primary" href="/docs/">Get started</a>
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
{:else}
  <main id="main" class="product-overview">
    <section class="product-intro">
      <p class="feature-eyebrow">Polymux product</p>
      <h1>A place to talk with your agents.</h1>
      <p>The native macOS app connects to your agent server. Explore conversations and agent capabilities below, then check the installation guide for build availability.</p>
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
{/if}

<SiteFooter />
