<script lang="ts">
  let { text, dismiss }: { text: string; dismiss: () => void } = $props();
</script>

<div class="warning glass" role="status">
  <svg aria-hidden="true" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7"><path d="M12 3 2 20h20L12 3ZM12 9v5m0 3v1"></path></svg>
  <span>{text}</span>
  <button aria-label="Dismiss warning" title="Dismiss" onclick={dismiss}><svg aria-hidden="true" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 5l14 14M19 5 5 19"></path></svg></button>
</div>

<style>
  .warning { display: flex; align-items: center; gap: 10px; max-width: 550px; padding: 6px 12px; border-radius: var(--radius-bubble); font-size: 12px; }
  button { display: grid; place-items: center; }
</style>
