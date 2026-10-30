<script lang="ts">
  import type {ProductFeatureIcon} from '../../lib/content/types';

  let {name}: {name: ProductFeatureIcon} = $props();
</script>

<svg class="feature-icon" viewBox="0 0 24 24" fill="none" aria-hidden="true">
  {#if name === 'workspace'}
    <rect x="3" y="4" width="18" height="16" rx="2" /><path d="M9 4v16M9 10h12" />
  {:else if name === 'hub'}
    <path d="M4 5h16v11H8l-4 3V5Z" /><path d="m5 7 7 5 7-5" />
  {:else if name === 'drive'}
    <path d="M3 7.5A2.5 2.5 0 0 1 5.5 5H10l2 2h6.5A2.5 2.5 0 0 1 21 9.5v7A2.5 2.5 0 0 1 18.5 19h-13A2.5 2.5 0 0 1 3 16.5v-9Z" />
  {:else if name === 'browser'}
    <circle cx="12" cy="12" r="9" /><path d="M3 12h18M12 3c2.5 2.5 3.5 5.5 3.5 9s-1 6.5-3.5 9M12 3C9.5 5.5 8.5 8.5 8.5 12s1 6.5 3.5 9" />
  {:else if name === 'memory'}
    <path d="M9 4.5A3 3 0 0 0 6 7.4 3.5 3.5 0 0 0 5.5 14 3 3 0 0 0 9 18.5c.7 1.3 1.7 2 3 2V3.5c-1.3 0-2.3.7-3 1Z" /><path d="M15 4.5a3 3 0 0 1 3 2.9 3.5 3.5 0 0 1 .5 6.6 3 3 0 0 1-3.5 4.5c-.7 1.3-1.7 2-3 2V3.5c1.3 0 2.3.7 3 1ZM7 10h2M15 9h2M8 16h2M14 14h3" />
  {:else if name === 'tasks'}
    <rect x="4" y="4" width="16" height="16" rx="2" /><path d="m8 12 2.5 2.5L16 9M8 7h8" />
  {:else if name === 'models'}
    <path d="m12 3 1.4 4.1L17.5 8.5l-4.1 1.4L12 14l-1.4-4.1-4.1-1.4 4.1-1.4L12 3Z" /><path d="m18.5 14 1 2.5L22 17.5l-2.5 1-1 2.5-1-2.5-2.5-1 2.5-1 1-2.5ZM5.5 13l.8 2.2 2.2.8-2.2.8L5.5 19l-.8-2.2-2.2-.8 2.2-.8.8-2.2Z" />
  {:else}
    <path d="M4 4h6v6H4zM14 4h6v6h-6zM4 14h6v6H4z" /><path d="M17 13v8M13 17h8" />
  {/if}
</svg>
