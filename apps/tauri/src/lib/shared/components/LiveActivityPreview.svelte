<script lang="ts">
  import {onMount} from 'svelte';
  import {fade} from 'svelte/transition';
  import type {ActivityPreviewRequestDto} from '@polymux/protocol';
  import {polymuxApi} from '../../api/polymux';
  import Icon from './Icon.svelte';
  import {t} from '../../../i18n';

  export let source: ActivityPreviewRequestDto;

  const api = polymuxApi();

  let image = '';
  let unavailable = false;
  let mounted = false;
  let sourceKey = '';
  let timer = 0;
  let generation = 0;
  let misses = 0;

  $: nextSourceKey = source.kind === 'browser' ? `browser:${source.tabId}` : `computer:${source.runId}`;
  $: if (mounted && nextSourceKey !== sourceKey) restart();

  function restart(): void {
    sourceKey = nextSourceKey;
    generation += 1;
    window.clearTimeout(timer);
    image = '';
    unavailable = false;
    misses = 0;
    void refresh(generation);
  }

  async function refresh(requestGeneration: number): Promise<void> {
    const ready = source.kind === 'computer' ? Boolean(source.runId) : Boolean(source.tabId);
    if (ready) {
      try {
        const next = await api.activity.preview(source);
        if (requestGeneration !== generation) return;
        if (next) {
          image = next;
          unavailable = false;
          misses = 0;
        } else {
          misses += 1;
          if (misses >= 2) {
            image = '';
            unavailable = true;
          }
        }
      } catch {
        if (requestGeneration !== generation) return;
        misses += 1;
        if (misses >= 2) {
          image = '';
          unavailable = true;
        }
      }
    }
    if (requestGeneration !== generation) return;
    timer = window.setTimeout(() => void refresh(requestGeneration), source.kind === 'browser' ? 900 : 1_200);
  }

  onMount(() => {
    mounted = true;
    restart();
    return () => {
      mounted = false;
      generation += 1;
      window.clearTimeout(timer);
    };
  });
</script>

<span
  class="live-activity-preview"
  class:unavailable
  aria-label={unavailable ? $t('activity.previewUnavailable') : $t('activity.livePreview')}
>
  {#if image}
    <img in:fade={{duration: 140}} src={image} alt="" draggable="false"/>
  {:else}
    <span class="live-activity-preview-placeholder" aria-hidden="true">
      <Icon name={source.kind === 'browser' ? 'globe' : 'computer'} size={20}/>
    </span>
  {/if}
</span>

<style>
  .live-activity-preview { display: block; width: 100%; min-width: 0; overflow: hidden; border-radius: 8px; }
  img { display: block; width: 100%; height: auto; }
  .live-activity-preview-placeholder { display: flex; align-items: center; justify-content: center; aspect-ratio: 16 / 9; background: var(--neutral-100); color: var(--neutral-500); }
</style>
