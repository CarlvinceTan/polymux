<script lang="ts">
  import type {LegalProps} from '../../../lib/content/types';
  import {formatDate} from '../../lib/format';
  import SiteHeader from '../../components/SiteHeader.svelte';
  import SiteFooter from '../../components/SiteFooter.svelte';

  let {title, updated, html}: LegalProps = $props();
</script>

<SiteHeader />
<main id="main" class="legal-page">
  <div class="title-block"><p class="eyebrow">Legal</p><h1>{title}</h1>{#if updated}<p class="effective">Updated {formatDate(updated)}</p>{/if}</div>
  <article class="prose">{@html html}</article>
</main>
<SiteFooter />
