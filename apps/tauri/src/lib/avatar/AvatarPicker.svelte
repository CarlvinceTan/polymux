<script lang="ts">
  import { untrack } from 'svelte';
  import type { Agent, AvatarDesign } from '../api/contract';
  import AgentAvatar from './AgentAvatar.svelte';
  import { categories, colors } from './choices';
  import { AVATAR_COLORS } from './palette';

  let { design, agent, onChange }: { design: AvatarDesign; agent: Agent; onChange: (design: AvatarDesign) => void } = $props();
  let category = $state<keyof typeof categories>(untrack(() =>
    (Object.keys(categories) as (keyof typeof categories)[]).find((key) => categories[key].some(([id]) => id === design.shape)) ?? 'Shapes',
  ));
</script>

<div class="picker">
  <div class="categories" role="group" aria-label="Category">
    {#each Object.keys(categories) as key (key)}
      <button class:chosen={category === key} aria-pressed={category === key} onclick={() => category = key as keyof typeof categories}>{key}</button>
    {/each}
  </div>
  <div class="shapes">
    {#each categories[category] as [id, label] (id)}
      <button class:chosen={design.shape === id} aria-label={label} aria-pressed={design.shape === id} title={label} onclick={() => onChange({ ...design, shape: id })}>
        <AgentAvatar agent={{ ...agent, avatar: { ...design, shape: id } }} size={40} />
      </button>
    {/each}
  </div>
  {#if category === 'Shapes'}
    <div class="colors">
      {#each colors as [id, label] (id)}
        <button aria-label={label} aria-pressed={design.color === id} title={label} class:chosen={design.color === id} onclick={() => onChange({ ...design, color: id })}>
          <span style:background={id === 'encre' ? 'linear-gradient(135deg, white 50%, black 50%)' : AVATAR_COLORS[id]}></span>
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .picker{display:flex;flex-direction:column;gap:12px;align-items:center;width:100%;min-width:0}
  .categories{display:flex;gap:4px;overflow:auto;max-width:100%;flex:none}
  .categories button{padding:6px 10px;border-radius:15px;white-space:nowrap}
  .categories button.chosen{background:var(--row-selected)}
  .shapes{display:grid;grid-template-columns:repeat(5,52px);gap:4px 8px;justify-content:center}
  .shapes button{height:48px;border:1px solid transparent;border-radius:10px;display:grid;place-items:center}
  .shapes button.chosen{background:var(--row-hover);border-color:var(--text-secondary)}
  .colors{display:flex;flex-wrap:wrap;justify-content:center;max-width:300px;gap:4px 12px}
  .colors button{width:36px;height:36px;border:2px solid transparent;border-radius:50%;display:grid;place-items:center}
  .colors button.chosen{border-color:var(--text)}
  .colors span{width:28px;height:28px;border-radius:50%;box-shadow:inset 0 0 0 1px rgba(128,128,128,.25)}
</style>
