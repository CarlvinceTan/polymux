<script lang="ts">
  let {size = 48}: {size?: number} = $props();
</script>

<svg
  class="account-mark"
  width={size}
  height={size}
  viewBox="0 0 200 200"
  aria-hidden="true"
>
  <path
    fill="currentColor"
    d="M 40 40 L 120 40 A 60 60 0 0 1 120 160 L 40 160 L 70 120 L 110 120 A 20 20 0 0 0 110 80 L 70 80 Z"
  />
</svg>

<style>
  .account-mark { display: block; flex: none; color: var(--neutral-950); }
</style>
