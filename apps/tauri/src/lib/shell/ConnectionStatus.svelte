<script lang="ts">
  import WarningBubble from './WarningBubble.svelte';
  let { connected, dismissed = $bindable(false) }: { connected: boolean; dismissed?: boolean } = $props();
  let shown = $state(false);
  let stalled = $state(false);
  let everConnected = $state(false);

  $effect(() => {
    if (connected) {
      everConnected = true;
      shown = false;
      stalled = false;
      dismissed = false;
      return;
    }
    const show = setTimeout(() => { shown = true; }, 3000);
    const stall = setTimeout(() => { stalled = true; }, 12000);
    return () => { clearTimeout(show); clearTimeout(stall); };
  });
</script>

{#if shown && !dismissed && !connected}
  <WarningBubble text={everConnected || stalled ? 'Can’t reach server' : 'Connecting…'} dismiss={() => (dismissed = true)} />
{/if}
