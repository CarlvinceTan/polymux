<script lang="ts">
  import type {TeamHostDto} from '@polymux/protocol';
  import Icon from '../../shared/components/Icon.svelte';
  import OpenMenu from '../../shared/components/OpenMenu.svelte';
  export let devices: TeamHostDto[] = [];
  export let deviceId = '';
  export let locked = false;
  export let onChange: (id: string) => void = () => {};
  let anchor: {rect: DOMRect} | null = null;
  $: selected = deviceId ? devices.find(device => device.hostId === deviceId) : devices.find(device => device.mode === 'local');
</script>
{#if devices.some(device => device.mode === 'remote') || deviceId && !selected}
  <button type="button" aria-label="Assistant device" aria-haspopup="menu" aria-expanded={Boolean(anchor)} disabled={locked}
    data-tooltip-label={locked ? 'Choose another device in a new chat' : 'Assistant device'} onclick={event => anchor = {rect: event.currentTarget.getBoundingClientRect()}}>
    <Icon name="devices" size={14}/><span>{selected?.deviceName ?? 'Device unavailable'}</span>
  </button>
  <OpenMenu {anchor} ariaLabel="Assistant device" choices={devices.filter(device => device.mode === 'local' || device.state === 'connected').map(device => ({value: device.hostId, label: device.deviceName, icon: device.hostId === selected?.hostId ? 'check' : 'computer'}))} onChoose={id => { onChange(id); anchor = null; }} onClose={() => anchor = null}/>
{/if}
<style>button{display:inline-flex;align-items:center;gap:6px;border:0;padding:0;background:transparent;color:var(--neutral-600);font:inherit;font-size:11px;cursor:pointer}button:hover{color:var(--neutral-900)}button:disabled{cursor:default}span{max-width:110px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}</style>
