<script lang="ts">
  import Icon from './Icon.svelte';

  export let value = '';
  export let placeholder = '';
  export let label = placeholder;
  export let clearLabel = 'Clear search';
  export let input: HTMLInputElement | null = null;
  export let onkeydown: ((event: KeyboardEvent) => void) | undefined = undefined;

  function clear(): void {
    value = '';
    input?.focus();
  }
</script>

<label class="polymux-search-field">
  <input
    bind:this={input}
    bind:value
    type="search"
    {placeholder}
    aria-label={label}
    {onkeydown}
  />
  {#if value}
    <button type="button" aria-label={clearLabel} data-tooltip="none" onclick={clear}>
      <Icon name="close" size={11} />
    </button>
  {/if}
</label>

<style>
  .polymux-search-field {
    position: relative;
    min-width: 0;
    display: flex;
    align-items: center;
    flex: 1;
  }

  input {
    -webkit-appearance: none;
    appearance: none;
    width: 100%;
    padding-inline-end: 28px !important;
  }

  input::-webkit-search-cancel-button {
    display: none;
    -webkit-appearance: none;
    appearance: none;
  }

  button {
    position: absolute;
    inset-inline-end: 4px;
    width: 20px;
    height: 20px;
    display: grid;
    place-items: center;
    border: 0;
    border-radius: 6px;
    padding: 0;
    background: transparent;
    color: var(--neutral-400);
    cursor: pointer;
  }

  button:hover,
  button:focus-visible {
    outline: 0;
    color: var(--neutral-950);
  }
</style>
