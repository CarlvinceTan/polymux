<script lang="ts">
  import {HOST_PAIRING_CODE_LENGTH, sanitizeHostPairingCodeInput} from '@polymux/protocol';
  let {value = $bindable(''), disabled = false}: {value?: string; disabled?: boolean} = $props();
</script>
<div class="device-code-input">
  <input aria-label="Device pairing code" inputmode="text" autocapitalize="characters" autocomplete="one-time-code" spellcheck="false" maxlength={HOST_PAIRING_CODE_LENGTH + 4}
    {disabled} value={value} oninput={(event) => value = sanitizeHostPairingCodeInput(event.currentTarget.value)}/>
  <div class="digits" aria-hidden="true">{#each Array(HOST_PAIRING_CODE_LENGTH) as _, index (index)}<span class:filled={Boolean(value[index])} class:current={index === value.length}>{value[index] ?? ''}</span>{/each}</div>
</div>
<style>
  .device-code-input{position:relative;width:100%}input{position:absolute;inset:0;width:100%;height:100%;opacity:.01;border:0;z-index:1;caret-color:transparent;font-size:16px}.digits{display:flex;gap:4px}.digits span{flex:1;min-width:0;height:36px;display:grid;place-items:center;border:1px solid var(--neutral-250,var(--neutral-300));border-radius:5px;background:var(--app-bg);color:var(--neutral-900);font-size:16px;font-variant-numeric:tabular-nums}.device-code-input:focus-within .current{outline:2px solid var(--focus-ring);outline-offset:1px}
</style>
