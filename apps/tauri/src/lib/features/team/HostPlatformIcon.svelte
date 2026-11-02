<script lang="ts">
  import apple from 'simple-icons/icons/apple.svg?url';
  import linux from 'simple-icons/icons/linux.svg?url';
  type SupportedPlatform = 'macos' | 'windows' | 'linux';

  let {platform, size = 18}: {platform: SupportedPlatform; size?: number} = $props();
  let mask = $derived(platform === 'macos' ? apple : linux);
</script>

{#if platform === 'windows'}
  <svg
    class="platform-icon"
    style={`width:${size}px;height:${size}px`}
    viewBox="0 0 24 24"
    aria-hidden="true"
  >
    <path d="M3 4.25 10.75 3.2v8.15H3v-7.1Zm8.85-1.2L21 1.8v9.55h-9.15v-8.3ZM3 12.55h7.75v8.2L3 19.7v-7.15Zm8.85 0H21v9.65l-9.15-1.3v-8.35Z" />
  </svg>
{:else}
  <span
    class="platform-icon platform-icon-mask"
    class:apple={platform === 'macos'}
    style={`width:${size}px;height:${size}px;--platform-icon:url("${mask}")`}
    aria-hidden="true"
  ></span>
{/if}

<style>
  .platform-icon {
    display: block;
    flex: 0 0 auto;
    color: inherit;
  }

  svg.platform-icon {
    fill: currentColor;
  }

  .apple { transform: translateY(-1px); }

  .platform-icon-mask {
    background: currentColor;
    -webkit-mask: var(--platform-icon) center / contain no-repeat;
    mask: var(--platform-icon) center / contain no-repeat;
  }
</style>
