<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import type { Agent } from '../api/contract';
  import type { Session } from '../session.svelte';
  import AgentAvatar from './AgentAvatar.svelte';
  import AvatarPicker from './AvatarPicker.svelte';
  import { avatarEdit } from './avatarEdit';

  let { session, agent, onClose }: { session: Session; agent: Agent; onClose: () => void } = $props();
  const owner = untrack(() => session);
  const id = untrack(() => agent.id);
  const store = untrack(() => avatarEdit(owner.address, agent, owner.client, (avatar) => {
    owner.agents = owner.agents.map((entry) => entry.id === id ? { ...entry, avatar } : entry);
  }));
  let design = $state(store.value);
  let issue = $state('');
  let dialog: HTMLDivElement | undefined = $state();

  onMount(() => {
    dialog?.focus();
    return store.connect((value, message) => { design = value; issue = message; });
  });
</script>

<div class="shade" role="presentation" onclick={onClose}>
  <div class="panel" role="dialog" aria-modal="true" aria-label="{agent.name}’s avatar" tabindex="-1" bind:this={dialog}
    onclick={(event) => event.stopPropagation()} onkeydown={(event) => { if (event.key === 'Escape') onClose(); }}>
    <header>
      <strong>{agent.name}’s avatar</strong>
      <button class="close glass" aria-label="Close avatar editor" onclick={onClose}>×</button>
    </header>
    <div class="body">
      <AgentAvatar agent={{ ...agent, avatar: design }} size={120} />
      <AvatarPicker {design} {agent} onChange={(value) => store.edit(value)} />
      {#if issue}<span class="issue" role="status">{issue}</span>{/if}
    </div>
  </div>
</div>

<style>
  .shade{position:fixed;inset:0;z-index:150;display:grid;place-items:center;background:rgba(0,0,0,.25)}
  .panel{width:min(460px,calc(100vw - 48px));height:min(560px,calc(100vh - 48px));border-radius:24px;background:var(--window-bg);display:flex;flex-direction:column;overflow:hidden;box-shadow:0 12px 40px rgba(0,0,0,.24)}
  header{height:76px;flex:none;display:grid;place-items:center;position:relative}
  header strong{font-size:13px;font-weight:600}
  .close{position:absolute;right:16px;top:20px;width:36px;height:36px;border-radius:50%;font-size:19px}
  .body{overflow:auto;flex:1;min-height:0;padding:8px 24px 24px;display:flex;flex-direction:column;gap:20px;align-items:center}
  .issue{color:var(--text-muted);font-size:12px;text-align:center}
</style>
