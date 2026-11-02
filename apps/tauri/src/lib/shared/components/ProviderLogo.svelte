<script lang="ts">
  import {providerMark, providerName} from '../options/providerBrands';
  export let provider: string;
  export let size = 18;
  export let logoDataUrl: string | undefined = undefined;
  let logoUrl: string | undefined;
  let logoLoad = 0;

  // Avatar convention: initials while we still know what the thing is called,
  // and the neutral glyph only for something genuinely unnamed — pseudo-initials
  // derived from nothing read as a glitch. Every branded provider resolves
  // before this, so the fallback is for custom providers and labs no icon set
  // covers.
  function initials(name: string): string {
    const words = name.trim().split(/\s+/).filter(Boolean);
    if (words.length > 1) return (words[0]![0]! + words[1]![0]!).toUpperCase();
    return (words[0] ?? '').slice(0, 2);
  }

  function resolveLogo(providerId: string, explicit: string | undefined): void {
    const request = ++logoLoad;
    if (explicit) {
      logoUrl = explicit;
      return;
    }
    logoUrl = undefined;
    // The complete provider catalogue contains hundreds of possible icon file
    // names. Keep that lookup out of the first UI bundle; the synchronous brand
    // mark below is a stable placeholder while this cached module loads.
    void import('../options/providerLogoAssets').then(({providerLogoUrl}) => {
      if (request === logoLoad) logoUrl = providerLogoUrl(providerId);
    }).catch(() => {});
  }

  $: mark = providerMark(provider);
  $: resolveLogo(provider, logoDataUrl);
  $: label = providerName(provider);
  $: initialsText = label === 'xAI' ? 'xAI' : initials(label);
</script>

<span class="provider-logo" style={`--logo-size:${size}px`} title={label} aria-label={label}>
  {#if logoUrl}
    <img src={logoUrl} alt="" aria-hidden="true"/>
  {:else if mark}
    <svg viewBox="0 0 24 24" role="img" aria-hidden="true" style={`color:#${mark.hex}`}><path d={mark.path} fill="currentColor"/></svg>
  {:else if initialsText}
    <span class="provider-fallback" aria-hidden="true"><span class="provider-wordmark">{initialsText}</span></span>
  {:else}
    <span class="provider-fallback" aria-hidden="true">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="6" r="2.3"/><circle cx="6" cy="16.5" r="2.3"/><circle cx="18" cy="16.5" r="2.3"/>
        <path d="M10.4 7.9 7.4 14.3M13.6 7.9l3 6.4M8.3 16.5h7.4"/>
      </svg>
    </span>
  {/if}
</span>

<style>
  .provider-logo{width:var(--logo-size);height:var(--logo-size);display:grid;flex:none;place-items:center;color:var(--neutral-800)}
  svg,img{width:100%;height:100%;display:block;object-fit:contain}
  /* Filled so the placeholder reads as deliberate rather than as a logo that
     failed to load — bare letters next to real brand marks look broken. */
  .provider-fallback{width:100%;height:100%;display:grid;place-items:center;border-radius:calc(var(--logo-size) * .28);background:var(--neutral-100);color:var(--neutral-500)}
  .provider-fallback svg{width:76%;height:76%}
  .provider-wordmark{font-size:calc(var(--logo-size) * .42);font-weight:700;letter-spacing:-.06em;line-height:1;color:var(--neutral-600)}
</style>
