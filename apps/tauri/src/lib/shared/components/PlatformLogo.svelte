<script module lang="ts">
  /** Every platform that has a mark here. Exported because callers outside the
   * hub — the agent activity trail among them — name a platform of their own
   * and need the same list to be sure it can be drawn. */
  export type Platform =
    | 'whatsapp' | 'telegram' | 'signal' | 'slack' | 'messenger' | 'instagram'
    | 'linkedin' | 'googlechat' | 'gmessages' | 'twitter' | 'bluesky' | 'gvoice'
    | 'zulip' | 'imessage' | 'wechat' | 'matrix' | 'mail';
</script>

<script lang="ts">
  import type {SimpleIcon} from 'simple-icons';
  import {
    siBluesky,
    siImessage,
    siGooglechat,
    siGooglemessages,
    siInstagram,
    siMatrix,
    siMessenger,
    siSignal,
    siTelegram,
    siX,
    siWechat,
    siWhatsapp,
    siZulip,
  } from 'simple-icons';

  interface Props {
    platform: Platform;
    size?: number;
    /** Renders a neutral monochrome glyph instead of the brand colours. */
    mono?: boolean;
  }

  type Mark = Pick<SimpleIcon, 'hex' | 'path'> & {rule?: 'evenodd'};

  let {platform, size = 20, mono = false}: Props = $props();

  // Gradients live in the document-wide id namespace, so a fixed id would make
  // every instance on a page resolve to whichever one mounted first.
  const uid = $props.id();

  // LinkedIn was pulled from simple-icons v16 over trademark, and 'mail' is the
  // generic mail-app idea rather than a brand, so both are authored here on the
  // same full-bleed 24x24 grid the simple-icons marks use.
  const linkedin: Mark = {
    hex: '0A66C2',
    path: 'M4.4 2.4a2.6 2.6 0 1 0 .01 0ZM2 8.9h4.8V22H2zM8.7 8.9h4.6v1.8c.8-1.3 2.3-2.2 4.2-2.2 3.1 0 5 2 5 5.7V22h-4.8v-6.8c0-1.8-.8-2.9-2.3-2.9-1.6 0-2.7 1.1-2.7 2.9V22H8.7z',
  };
  const mail: Mark = {
    hex: '0A5BD8',
    rule: 'evenodd',
    path: 'M4 4h16a3 3 0 0 1 3 3v10a3 3 0 0 1-3 3H4a3 3 0 0 1-3-3V7a3 3 0 0 1 3-3ZM3.6 5.5 12 12l8.4-6.5 1.4 1.8-9.1 7a1.3 1.3 0 0 1-1.6 0L2.2 7.3Z',
  };

  // Slack and Google Voice are absent from simple-icons v16, so both are
  // authored here on the same full-bleed 24x24 grid the other marks use.
  const slack: Mark = {
    hex: '611F69',
    path: 'M5.1 15.2a2.1 2.1 0 1 1-2.1-2.1h2.1zm1 0a2.1 2.1 0 0 1 4.2 0v5.2a2.1 2.1 0 0 1-4.2 0zM8.2 6.9a2.1 2.1 0 1 1 2.1-2.1v2.1zm0 1.1a2.1 2.1 0 0 1 0 4.2H3a2.1 2.1 0 0 1 0-4.2zM16.8 10.1a2.1 2.1 0 1 1 2.1 2.1h-2.1zm-1.1 0a2.1 2.1 0 0 1-4.2 0V4.8a2.1 2.1 0 0 1 4.2 0zM13.6 18.4a2.1 2.1 0 1 1-2.1 2.1v-2.1zm0-1a2.1 2.1 0 0 1 0-4.2h5.3a2.1 2.1 0 0 1 0 4.2z',
  };
  const gvoice: Mark = {
    hex: '1A73E8',
    path: 'M12.6 1.7 22.3 11.4a1.9 1.9 0 0 1 0 2.7l-8.2 8.2a1.9 1.9 0 0 1-2.7 0L1.7 12.6a1.9 1.9 0 0 1 0-2.7l8.2-8.2a1.9 1.9 0 0 1 2.7 0M9.4 7.2a1 1 0 0 0-1.4 0l-1 1a1.4 1.4 0 0 0-.2 1.7 15 15 0 0 0 5.9 5.9 1.4 1.4 0 0 0 1.7-.2l1-1a1 1 0 0 0 0-1.4l-1.6-1.6a1 1 0 0 0-1.4 0l-.6.6a10 10 0 0 1-2.4-2.4l.6-.6a1 1 0 0 0 0-1.4z',
  };

  const MARKS: Record<Platform, Mark> = {
    whatsapp: siWhatsapp,
    telegram: siTelegram,
    signal: siSignal,
    slack,
    messenger: siMessenger,
    instagram: siInstagram,
    linkedin,
    googlechat: siGooglechat,
    gmessages: siGooglemessages,
    twitter: siX,
    bluesky: siBluesky,
    gvoice,
    zulip: siZulip,
    imessage: siImessage,
    wechat: siWechat,
    // Matrix ships as pure black, which vanishes against a dark surface.
    matrix: {...siMatrix, hex: '2B3038'},
    mail,
  };

  // The marks run edge to edge, so the tile treatment insets them to roughly
  // the proportion an app icon uses; mono trims them a little to sit at the
  // same optical weight as Icon.svelte's glyphs.
  const INSET = 'translate(12 12) scale(.58) translate(-12 -12)';
  const TRIM = 'translate(12 12) scale(.88) translate(-12 -12)';

  let mark = $derived(MARKS[platform]);
  let tile = $derived(platform === 'mail' ? `url(#${uid}-mail)` : `#${mark.hex}`);
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  aria-hidden="true"
  focusable="false"
  data-platform={platform}
>
  {#if mono}
    <path d={mark.path} fill="currentColor" fill-rule={mark.rule} transform={TRIM}/>
  {:else}
    {#if platform === 'mail'}
      <defs>
        <linearGradient id="{uid}-mail" x1="12" y1="0" x2="12" y2="24" gradientUnits="userSpaceOnUse">
          <stop offset="0" stop-color="#5CADFF"/>
          <stop offset="1" stop-color="#0A5BD8"/>
        </linearGradient>
      </defs>
    {/if}
    <rect x=".5" y=".5" width="23" height="23" rx="6" fill={tile}/>
    <path d={mark.path} fill="#fff" fill-rule={mark.rule} transform={INSET}/>
  {/if}
</svg>
