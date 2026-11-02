<script lang="ts">
  import type {HTMLButtonAttributes} from 'svelte/elements';
  import Icon from './Icon.svelte';
  import {MAIN_UI_ICON_SIZE, MAIN_UI_ICON_STROKE_WIDTH} from '../layout/iconSizing';

  type IconName = import('svelte').ComponentProps<typeof Icon>['name'];

  let {
    icon = null,
    chevron = false,
    chevronLeft = false,
    danger = false,
    active = false,
    checked = false,
    role = 'menuitem',
    el = $bindable<HTMLButtonElement | null>(null),
    children,
    ...rest
  }: {
    icon?: IconName | null;
    chevron?: boolean;
    chevronLeft?: boolean;
    danger?: boolean;
    active?: boolean;
    checked?: boolean;
    role?: 'menuitem' | 'menuitemradio' | 'menuitemcheckbox';
    el?: HTMLButtonElement | null;
    children: import('svelte').Snippet;
  } & Omit<HTMLButtonAttributes, 'children' | 'role'> = $props();
</script>

<button
  bind:this={el}
  type="button"
  class={['polymux-dropdown-item', {danger, 'submenu-open': active}]}
  {role}
  aria-checked={role === 'menuitem' ? undefined : checked}
  aria-haspopup={chevron ? 'menu' : undefined}
  aria-expanded={chevron ? active : undefined}
  {...rest}
>
  {#if icon}
    <span class="menu-item-icon"><Icon name={icon} size={MAIN_UI_ICON_SIZE} strokeWidth={MAIN_UI_ICON_STROKE_WIDTH}/></span>
  {/if}
  <span>{@render children()}</span>
  {#if checked}<Icon name="check" size={13}/>{/if}
  {#if chevron}
    <span class={['menu-item-chevron', {left: chevronLeft}]}><Icon name="chevron" size={13}/></span>
  {/if}
</button>
