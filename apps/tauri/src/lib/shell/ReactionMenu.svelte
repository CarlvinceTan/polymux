<script lang="ts">
  import type { Reaction } from '../api/contract';

  let { point, reactions, onChoose, showChoices = true, busy = false, onCopy, onShare, canShare = false, onReply, onClose }: {
    point: { x: number; y: number };
    reactions: Reaction[];
    onChoose: (emoji: string) => void;
    showChoices?: boolean;
    busy?: boolean;
    onCopy?: () => void;
    onShare?: () => void;
    canShare?: boolean;
    onReply?: () => void;
    onClose: () => void;
  } = $props();

  const fixed = ['❤️', '👍', '👎', '😂', '‼️', '❓'];
  const defaults = ['😍', '😒', '👌', '😊', '😘'];
  const recent = [...new Set([...(localStorage.getItem('recentReactions') ?? '').split(','), ...defaults])]
    .filter((emoji) => emoji && !fixed.includes(emoji)).slice(0, 5);
  const left = $derived(Math.max(8, Math.min(point.x, window.innerWidth - 264)));
  const top = $derived(Math.max(8, Math.min(point.y + 6, window.innerHeight - 12 - (showChoices ? 78 : 0) - 32 * (+!!onCopy + +!!onShare + +!!onReply))));

  function choose(emoji: string) {
    if (!fixed.includes(emoji)) {
      localStorage.setItem('recentReactions', [emoji, ...recent.filter((item) => item !== emoji)].slice(0, 5).join(','));
    }
    onChoose(emoji);
  }

  function keys(event: KeyboardEvent) {
    if (event.key === 'Escape') { event.preventDefault(); onClose(); return; }
    if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(event.key)) return;
    const buttons = Array.from(event.currentTarget instanceof HTMLElement ? event.currentTarget.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') : []);
    const index = buttons.indexOf(document.activeElement as HTMLButtonElement);
    const offset = event.key === 'ArrowLeft' ? -1 : event.key === 'ArrowRight' ? 1 : event.key === 'ArrowUp' ? -6 : 6;
    event.preventDefault();
    buttons[Math.max(0, Math.min(buttons.length - 1, index + offset))]?.focus();
  }
</script>

<div class="shade" role="presentation" onpointerdown={onClose} onwheel={onClose}></div>
<div class="menu polymux-dropdown-menu" role="menu" aria-label={showChoices ? 'Reactions' : 'Message actions'} tabindex="-1" style:left="{left}px" style:top="{top}px" onkeydown={keys}
  {@attach (element) => { element.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus(); }}>
  {#if showChoices}
  <div class="choices">
    {#each [...fixed, ...recent] as emoji (emoji)}
      <button role="menuitemcheckbox" aria-label="React {emoji}" disabled={busy} aria-checked={reactions.some((reaction) => reaction.emoji === emoji && !reaction.agentID)} onclick={() => choose(emoji)}>{emoji}</button>
    {/each}
  </div>
  {/if}
  {#if onCopy}<button class="action" role="menuitem" onclick={onCopy}><svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><rect x="8" y="7" width="11" height="14" rx="2" /><path d="M16 7V5a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h2" /></svg>Copy</button>{/if}
  {#if onShare}<button class="action" role="menuitem" disabled={!canShare} onclick={onShare}><svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><path d="M12 15V3m-4 4 4-4 4 4M6 11v8a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2v-8" /></svg>Share</button>{/if}
  {#if onReply}<button class="action" role="menuitem" onclick={onReply}><svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><path d="m10 8-5 4 5 4M5 12h8a6 6 0 0 1 6 6" /></svg>Reply</button>{/if}
</div>

<style>
  .shade { position: fixed; inset: 0; z-index: 30; }
  .menu { position: fixed; z-index: 31; width: 256px; padding: 6px; box-sizing: border-box; }
  .choices { display: grid; grid-template-columns: repeat(6, 1fr); gap: 2px; }
  button { width: 38px; height: 38px; display: grid; place-items: center; border-radius: 7px; font-size: 23px; line-height: 1; }
  button:hover, button:focus-visible { background: var(--glass-field); }
  button[aria-checked='true'] { background: color-mix(in srgb, var(--text) 14%, transparent); }
  .action { width: 100%; height: 32px; display: flex; gap: 8px; padding: 0 8px; font-size: 13px; }
  .action:disabled { opacity: .45; }
</style>
