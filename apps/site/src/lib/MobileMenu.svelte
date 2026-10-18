<script lang="ts">
  import {onMount} from 'svelte';
  import github from 'simple-icons/icons/github.svg?url';
  import DownloadIcon from './DownloadIcon.svelte';

  type Section = 'home' | 'docs' | 'releases';

  let {active, downloadHref = '/#download'}: {active?: Section; downloadHref?: string} = $props();
  let open = $state(false);
  let menuRoot: HTMLDivElement;

  const links: {id: Section; label: string; href: string}[] = [
    {id: 'home', label: 'Home', href: '/'},
    {id: 'docs', label: 'Docs', href: '/docs/'},
    {id: 'releases', label: 'Releases', href: '/releases/'},
  ];

  onMount(() => {
    const onDocumentClick = (event: MouseEvent) => {
      if (open && !event.composedPath().includes(menuRoot)) open = false;
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') open = false;
    };
    window.addEventListener('click', onDocumentClick, true);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      window.removeEventListener('click', onDocumentClick, true);
      document.removeEventListener('keydown', onKeyDown);
    };
  });
</script>

<div class="mobile-menu" class:open bind:this={menuRoot}>
  <button
    class="mobile-menu-toggle"
    type="button"
    aria-label={open ? 'Close navigation menu' : 'Open navigation menu'}
    aria-expanded={open}
    aria-controls="mobile-navigation-drawer"
    onclick={() => open = !open}
  >
    <span></span><span></span><span></span>
  </button>

  <div id="mobile-navigation-drawer" class="mobile-menu-drawer" aria-hidden={!open} inert={!open}>
    <div class="mobile-menu-links" role="navigation" aria-label="Mobile navigation">
      {#each links as link (link.id)}
        <a
          class:current={link.id === active}
          href={link.href}
          aria-current={link.id === active ? 'page' : undefined}
          onclick={() => open = false}
        >{link.label}</a>
      {/each}
    </div>
    <div class="mobile-menu-actions">
      <a class="mobile-menu-download" href={downloadHref} onclick={() => open = false}>
        <DownloadIcon />
        <span>Download</span>
      </a>
      <a class="mobile-menu-github" href="https://github.com/CarlvinceTan/polymux" onclick={() => open = false}>
        <img src={github} alt="" />
        <span>GitHub</span>
      </a>
    </div>
  </div>
</div>

<style>
  .mobile-menu{position:relative;display:none;margin-left:auto;color:#171717}.mobile-menu-toggle{width:40px;height:40px;padding:9px;display:flex;align-items:center;justify-content:center;flex-direction:column;gap:4px;border:0;background:transparent;color:inherit;cursor:pointer}.mobile-menu-toggle span{width:20px;height:1.5px;display:block;border-radius:2px;background:currentColor;transform-origin:center;transition:transform 160ms ease,opacity 120ms ease}.open .mobile-menu-toggle span:first-child{transform:translateY(5.5px) rotate(45deg)}.open .mobile-menu-toggle span:nth-child(2){opacity:0}.open .mobile-menu-toggle span:last-child{transform:translateY(-5.5px) rotate(-45deg)}
  .mobile-menu-drawer{position:absolute;top:calc(100% + 14px);right:0;width:min(340px,calc(100vw - 28px));max-height:calc(100vh - 82px);padding:8px;overflow:auto;border:1px solid #d8d8d3;border-radius:10px;background:#fbfbf9;box-shadow:0 22px 60px rgba(0,0,0,.16);opacity:0;visibility:hidden;pointer-events:none;transform:translateX(8px);transition:opacity 160ms ease,transform 180ms cubic-bezier(.45,0,.55,1),visibility 160ms ease;scrollbar-width:none}.mobile-menu-drawer::-webkit-scrollbar{display:none}.open .mobile-menu-drawer{opacity:1;visibility:visible;pointer-events:auto;transform:translateX(0)}.mobile-menu-links{display:flex;align-items:stretch;flex-direction:column;gap:2px;color:#666661}.mobile-menu-links a,.mobile-menu-actions a{min-height:48px;padding:0 14px;display:flex;align-items:center;border-radius:7px;font-size:15px;font-weight:600;line-height:1;transition:background 140ms ease,color 140ms ease}.mobile-menu-links a:hover,.mobile-menu-links a.current,.mobile-menu-github:hover{color:#171717;background:#efefeb}.mobile-menu-links a.current{font-weight:700}.mobile-menu-actions{margin-top:8px;padding-top:8px;display:flex;flex-direction:column;gap:2px;border-top:1px solid #deded9}.mobile-menu-actions a{gap:9px}.mobile-menu-download{color:#fff;background:#171717}.mobile-menu-download:hover{opacity:.84}.mobile-menu-download :global(.download-icon){width:17px;height:17px;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}.mobile-menu-github{color:#555550}.mobile-menu-github img{width:17px;height:17px;opacity:.78}
  @media(max-width:1050px){.mobile-menu{display:block}}
</style>
