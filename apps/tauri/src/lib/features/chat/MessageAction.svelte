<script module lang="ts">
  export type MessageActionIcon = 'fork' | 'share' | 'copy' | 'check' | 'edit' | 'thumb-up' | 'thumb-down';
</script>

<script lang="ts">
  import Icon from '../../shared/components/Icon.svelte';
  import {MAIN_UI_ICON_SIZE, MAIN_UI_ICON_STROKE_WIDTH} from '../../shared/layout/iconSizing';

  export let icon: MessageActionIcon;
  export let label: string;
  export let active = false;
  export let onAction: () => void;
</script>

<!-- The tooltip comes from the shared portaled layer so it is positioned against
     the viewport and cannot be clipped by the scrolling conversation column. -->
<span class="message-action-wrap">
  <button type="button" class:active aria-label={label} aria-pressed={active || undefined} onclick={onAction}>
    <Icon name={icon} size={MAIN_UI_ICON_SIZE} strokeWidth={MAIN_UI_ICON_STROKE_WIDTH} filled={active}/>
  </button>
</span>
