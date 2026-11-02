<script lang="ts">
  /** The sidebar's glass controls, trailing in the titlebar like the reference toolbar. */
  let {
    searching,
    collapsed,
    onSearch,
    onNewChat,
    onSettings,
    onReveal,
  }: {
    searching: boolean;
    collapsed: boolean;
    onSearch: () => void;
    onNewChat: (point: { x: number; y: number }) => void;
    onSettings: () => void;
    onReveal: () => void;
  } = $props();
</script>

<div class="bar" data-tauri-drag-region>
  {#if collapsed}
    <button class="solo glass-bar" aria-label="Show sidebar" onclick={onReveal}>
      <svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.7">
        <rect x="3" y="4" width="18" height="16" rx="3"></rect>
        <path d="M9 4v16"></path>
      </svg>
    </button>
  {:else}
    <div class="pair glass-bar">
      <button class="tool" class:on={searching} aria-label="Search" onclick={onSearch}>
        <svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.9">
          <circle cx="10.5" cy="10.5" r="6.5"></circle>
          <path d="m15.5 15.5 4 4"></path>
        </svg>
      </button>
      <button
        class="tool"
        aria-label="New chat"
        onclick={(event) => {
          const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
          onNewChat({ x: rect.left, y: rect.bottom + 6 });
        }}
      >
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.7">
          <path d="M12 5v14M5 12h14"></path>
        </svg>
      </button>
    </div>
    <button class="solo glass-bar" aria-label="Settings" onclick={onSettings}>
      <svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.6">
        <circle cx="12" cy="12" r="3"></circle>
        <path
          d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"
        ></path>
      </svg>
    </button>
  {/if}
</div>

<style>
  .bar {
    height: var(--titlebar-height);
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    padding: 0 12px 0 88px;
    box-sizing: border-box;
    flex: none;
  }
  .pair {
    display: flex;
    width: 72px;
    height: 36px;
    border-radius: 18px;
    overflow: hidden;
    box-sizing: border-box;
    flex: none;
  }
  .tool,
  .solo {
    width: 36px;
    height: 36px;
    display: grid;
    place-items: center;
    color: var(--text);
  }
  .solo {
    border-radius: 50%;
    box-sizing: border-box;
    flex: none;
  }
  .tool:hover,
  .solo:hover {
    background: var(--row-hover);
  }
  .tool.on {
    color: var(--unread-dot);
  }
</style>
