<script module lang="ts">
  export type FileAttachmentStatus = 'uploading' | 'done' | 'error';
</script>

<script lang="ts">
  import {t} from '../../../i18n';

  export let name: string;
  export let status: FileAttachmentStatus = 'done';
  export let progress = 100;
  export let removable = true;
  export let halfRow = false;
  export let onRemove: () => void = () => {};

  const radius = 5;
  const circumference = 2 * Math.PI * radius;
  $: boundedProgress = Math.min(100, Math.max(0, progress));
  $: dashOffset = circumference - (boundedProgress / 100) * circumference;
</script>

<span class:half-row={halfRow} class:is-error={status === 'error'} class="inline-file-chip">
  {#if status === 'uploading'}
    <svg class="inline-file-status upload-progress" viewBox="0 0 12 12" aria-hidden="true">
      <circle cx="6" cy="6" r={radius} fill="none" stroke="currentColor" stroke-width="1.5" class="progress-track"/>
      <circle
        cx="6"
        cy="6"
        r={radius}
        fill="none"
        stroke="currentColor"
        stroke-width="1.5"
        stroke-linecap="round"
        class="progress-value"
        stroke-dasharray={circumference}
        stroke-dashoffset={dashOffset}
      />
    </svg>
  {:else if status === 'error'}
    <svg class="inline-file-status error-icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
      <path fill-rule="evenodd" d="M10 18a8 8 0 1 0 0-16 8 8 0 0 0 0 16Zm0-11.75a.75.75 0 0 1 .75.75v3a.75.75 0 0 1-1.5 0V7a.75.75 0 0 1 .75-.75Zm0 7a.875.875 0 1 0 0 1.75.875.875 0 0 0 0-1.75Z" clip-rule="evenodd"/>
    </svg>
  {:else}
    <svg class="inline-file-status document-icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
      <path fill-rule="evenodd" d="M4.25 2A2.25 2.25 0 0 0 2 4.25v11.5A2.25 2.25 0 0 0 4.25 18h11.5A2.25 2.25 0 0 0 18 15.75V8.5h-5.75A2.25 2.25 0 0 1 10 6.25V2H4.25Zm7.25.19v4.06c0 .414.336.75.75.75h5.56a2.25 2.25 0 0 0-.47-.66l-3.68-3.68a2.25 2.25 0 0 0-2.16-.47Z" clip-rule="evenodd"/>
    </svg>
  {/if}

  <span class="inline-file-label" title={name}>{name}</span>

  {#if removable}
    <button
      type="button"
      class="inline-file-remove"
      aria-label={$t('common.remove', {name})}
      data-tooltip="none"
      onclick={(event) => {
        event.stopPropagation();
        onRemove();
      }}
    >
      <svg viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
        <path d="M5.22 5.22a.75.75 0 0 1 1.06 0L10 8.94l3.72-3.72a.75.75 0 1 1 1.06 1.06L11.06 10l3.72 3.72a.75.75 0 1 1-1.06 1.06L10 11.06l-3.72 3.72a.75.75 0 0 1-1.06-1.06L8.94 10 5.22 6.28a.75.75 0 0 1 0-1.06Z"/>
      </svg>
    </button>
  {/if}
</span>
