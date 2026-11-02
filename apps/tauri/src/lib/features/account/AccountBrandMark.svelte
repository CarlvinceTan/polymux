<script lang="ts">
  import googleColor from '@lobehub/icons-static-svg/icons/google-color.svg?url';
  import apple from 'simple-icons/icons/apple.svg?url';

  let {brand, size = 16}: {brand: 'google' | 'apple'; size?: number} = $props();
</script>

{#if brand === 'google'}
  <img
    class="account-brand-mark"
    src={googleColor}
    width={size}
    height={size}
    alt=""
    aria-hidden="true"
  />
{:else}
  <span
    class="account-brand-mark account-brand-apple"
    style:width={`${size}px`}
    style:height={`${size}px`}
    style:--account-brand={`url("${apple}")`}
    aria-hidden="true"
  ></span>
{/if}

<style>
  .account-brand-mark { display: block; flex: none; }
  .account-brand-apple {
    background: currentColor;
    -webkit-mask: var(--account-brand) center / contain no-repeat;
    mask: var(--account-brand) center / contain no-repeat;
    transform: translateY(-1px);
  }
</style>
