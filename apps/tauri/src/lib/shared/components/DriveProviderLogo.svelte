<script lang="ts">
  import type {DriveProviderId} from '@polymux/protocol';

  interface Props {
    provider: DriveProviderId;
    size?: number;
    /**
     * Renders the bare brand mark instead of a rounded app tile. Used for the
     * badge that trails a filename, where a filled tile at 11px reads as a
     * blob rather than a logo.
     */
    plain?: boolean;
  }

  type Mark = {hex: string; path: string; rule?: 'evenodd'};
  /**
   * A brand whose mark is its colours, not a silhouette. Google Drive's logo
   * has been six coloured segments since 2020, and a white knockout of it is
   * a different mark rather than a smaller one — so these are drawn as shipped
   * and the tile goes white underneath, which is how Google draws the app icon
   * itself.
   */
  type ColourMark = {
    tile: string;
    /** The mark's own viewBox, fitted to the 24x24 grid on render. */
    box: {width: number; height: number};
    paths: {d: string; fill: string}[];
  };

  let {provider, size = 20, plain = false}: Props = $props();

  const dropbox: Mark = {
    hex: '0061FF',
    path: 'M6 1.807L0 5.629l6 3.822 6.001-3.822L6 1.807zM18 1.807l-6 3.822 6 3.822 6-3.822-6-3.822zM0 13.274l6 3.822 6.001-3.822L6 9.452l-6 3.822zM18 9.452l-6 3.822 6 3.822 6-3.822-6-3.822zM6 18.371l6.001 3.822 6-3.822-6-3.822L6 18.371z',
  };
  // OneDrive and S3 are absent from simple-icons v16 — both were pulled over
  // trademark, the same reason LinkedIn and Slack are hand-authored in
  // PlatformLogo. They are drawn here on the same full-bleed 24x24 grid.
  const onedrive: Mark = {
    hex: '0078D4',
    path: 'M10.4 6.1a5.6 5.6 0 0 1 5.4 4.1 4.1 4.1 0 0 1 1-.1 4 4 0 0 1 .1 8H6.4a4.6 4.6 0 0 1-.8-9.1 5.6 5.6 0 0 1 4.8-2.9Z',
  };
  const s3: Mark = {
    hex: '569A31',
    rule: 'evenodd',
    // A storage bucket: the tapered drum every S3 mark is built from.
    path: 'M4.4 3h15.2l-1.5 17.5A1.7 1.7 0 0 1 16.4 22H7.6a1.7 1.7 0 0 1-1.7-1.5ZM6.6 7.4l.4 4.4h10l.4-4.4Z',
  };
  // The local drive is not a brand, so it gets a device rather than a logo —
  // and it is drawn filled like the others so every mark shares one weight.
  //
  // Two stacked bays with a status light each: the ordinary drive glyph, which
  // reads as storage at 16px in a way a monitor does not. The rings and the
  // lights both come out of one path — under evenodd the bay's inner cutout
  // clears the outer shape and the light sits inside that, filling again.
  const local: Mark = {
    hex: '52525B',
    rule: 'evenodd',
    path: [
      // Upper bay.
      'M4.5 3.5H19.5A2 2 0 0 1 21.5 5.5V9A2 2 0 0 1 19.5 11H4.5A2 2 0 0 1 2.5 9V5.5A2 2 0 0 1 4.5 3.5Z',
      'M5 5H19A1 1 0 0 1 20 6V8.5A1 1 0 0 1 19 9.5H5A1 1 0 0 1 4 8.5V6A1 1 0 0 1 5 5Z',
      'M6.5 6.35A.9.9 0 1 1 6.5 8.15A.9.9 0 1 1 6.5 6.35Z',
      // Lower bay, the same shape dropped by 9.5.
      'M4.5 13H19.5A2 2 0 0 1 21.5 15V18.5A2 2 0 0 1 19.5 20.5H4.5A2 2 0 0 1 2.5 18.5V15A2 2 0 0 1 4.5 13Z',
      'M5 14.5H19A1 1 0 0 1 20 15.5V18A1 1 0 0 1 19 19H5A1 1 0 0 1 4 18V15.5A1 1 0 0 1 5 14.5Z',
      'M6.5 15.85A.9.9 0 1 1 6.5 17.65A.9.9 0 1 1 6.5 15.85Z',
    ].join(''),
  };

  // Google Drive, as Google ships it: https://about.google/brand-resource-center
  const googleDrive: ColourMark = {
    tile: 'FFFFFF',
    box: {width: 87.3, height: 78},
    paths: [
      {d: 'm6.6 66.85 3.85 6.65c.8 1.4 1.95 2.5 3.3 3.3l13.75-23.8h-27.5c0 1.55.4 3.1 1.2 4.5z', fill: '#0066da'},
      {d: 'm43.65 25-13.75-23.8c-1.35.8-2.5 1.9-3.3 3.3l-25.4 44a9.06 9.06 0 0 0 -1.2 4.5h27.5z', fill: '#00ac47'},
      {d: 'm73.55 76.8c1.35-.8 2.5-1.9 3.3-3.3l1.6-2.75 7.65-13.25c.8-1.4 1.2-2.95 1.2-4.5h-27.502l5.852 11.5z', fill: '#ea4335'},
      {d: 'm43.65 25 13.75-23.8c-1.35-.8-2.9-1.2-4.5-1.2h-18.5c-1.6 0-3.15.45-4.5 1.2z', fill: '#00832d'},
      {d: 'm59.8 53h-32.3l-13.75 23.8c1.35.8 2.9 1.2 4.5 1.2h50.8c1.6 0 3.15-.45 4.5-1.2z', fill: '#2684fc'},
      {d: 'm73.4 26.5-12.7-22c-.8-1.4-1.95-2.5-3.3-3.3l-13.75 23.8 16.15 28h27.45c0-1.55-.4-3.1-1.2-4.5z', fill: '#ffba00'},
    ],
  };

  // The virtual drive is not a brand either: it is every connected place at
  // once, so it gets stacked plates rather than a logo — the same filled
  // weight as the local mark beside it.
  const all: Mark = {
    hex: '52525B',
    path: [
      'M12 2.75 21.5 7.5 12 12.25 2.5 7.5Z',
      'M12 14.4 4.6 10.7 2.5 11.75 12 16.5 21.5 11.75 19.4 10.7Z',
      'M12 18.65 4.6 14.95 2.5 16 12 20.75 21.5 16 19.4 14.95Z',
    ].join(''),
  };

  // Not a brand either: a share is somebody's server, so it gets a globe with
  // its latitudes drawn through — the same filled weight as the local bays and
  // the virtual plates it sits beside.
  const network: Mark = {
    hex: '52525B',
    rule: 'evenodd',
    path: [
      'M12 1.9A10.1 10.1 0 1 0 12 22.1A10.1 10.1 0 1 0 12 1.9ZM12 3.5A8.5 8.5 0 1 1 12 20.5A8.5 8.5 0 1 1 12 3.5Z',
      'M2.9 8.6H21.1V10.1H2.9Z',
      'M2.9 13.9H21.1V15.4H2.9Z',
      'M12 2.2C14.6 4.6 15.9 8.1 15.9 12C15.9 15.9 14.6 19.4 12 21.8C9.4 19.4 8.1 15.9 8.1 12C8.1 8.1 9.4 4.6 12 2.2ZM12 4.6C10.4 6.6 9.6 9.2 9.6 12C9.6 14.8 10.4 17.4 12 19.4C13.6 17.4 14.4 14.8 14.4 12C14.4 9.2 13.6 6.6 12 4.6Z',
    ].join(''),
  };

  const MARKS: Record<DriveProviderId, Mark | ColourMark> = {
    all,
    network,
    'google-drive': googleDrive,
    dropbox,
    onedrive,
    s3,
    local,
  };

  function isColour(mark: Mark | ColourMark): mark is ColourMark {
    return 'paths' in mark;
  }

  /** Fits a mark's own viewBox onto the 24x24 grid, centred. */
  function fit(box: {width: number; height: number}): string {
    const scale = 24 / Math.max(box.width, box.height);
    return `translate(${(24 - box.width * scale) / 2} ${
      (24 - box.height * scale) / 2
    }) scale(${scale})`;
  }

  // The marks run edge to edge, so the tile treatment insets them to roughly
  // the proportion an app icon uses — the same ratio PlatformLogo uses, so a
  // storage tile and a messaging tile sit at one optical weight.
  const INSET = 'translate(12 12) scale(.58) translate(-12 -12)';

  let mark = $derived(MARKS[provider]);
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  aria-hidden="true"
  focusable="false"
  data-drive-provider={provider}
>
  {#if isColour(mark)}
    <!-- A colour mark keeps its own palette; only the tile changes. -->
    {#if !plain}
      <rect x=".5" y=".5" width="23" height="23" rx="6" fill="#{mark.tile}"/>
    {/if}
    <g transform={plain ? fit(mark.box) : `${INSET} ${fit(mark.box)}`}>
      {#each mark.paths as segment (segment.d)}
        <path d={segment.d} fill={segment.fill}/>
      {/each}
    </g>
  {:else if plain}
    <path d={mark.path} fill="#{mark.hex}" fill-rule={mark.rule}/>
  {:else}
    <rect x=".5" y=".5" width="23" height="23" rx="6" fill="#{mark.hex}"/>
    <path d={mark.path} fill="#fff" fill-rule={mark.rule} transform={INSET}/>
  {/if}
</svg>
