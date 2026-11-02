<script lang="ts">
  import languagesIcon from '../../../assets/icons/languages.png';

  export let name:
    | 'summary' | 'panel' | 'panel-left' | 'screen-record' | 'computer' | 'devices' | 'tablet' | 'laptop' | 'server' | 'phone' | 'call' | 'apps' | 'plus' | 'close' | 'send' | 'mic' | 'smile' | 'waveform' | 'stop' | 'cursor'
    | 'attach' | 'options' | 'verified' | 'globe' | 'file' | 'task' | 'back'
    | 'forward' | 'reload' | 'download' | 'more' | 'ellipsis' | 'trash' | 'expand' | 'collapse' | 'chevron' | 'arrow-down' | 'arrow-up' | 'arrow-up-right' | 'switch'
    | 'bolt' | 'document-text' | 'paper-airplane' | 'history' | 'archive' | 'drive' | 'new-chat'
    | 'copy' | 'check' | 'circle-check' | 'circle-question' | 'bug' | 'edit' | 'signature' | 'thumb-up' | 'thumb-down' | 'search' | 'zoom-in' | 'zoom-out' | 'picture-in-picture' | 'terminal' | 'wrench' | 'book-open' | 'languages'
    | 'memory-cited' | 'brain' | 'compact' | 'compress' | 'sparkles' | 'bot'
    | 'connections' | 'mcp' | 'link' | 'puzzle' | 'grip' | 'steer' | 'play' | 'pause' | 'speaker' | 'speaker-off' | 'mic-off' | 'chat'
    | 'document' | 'presentation' | 'folder' | 'folder-open' | 'pdf' | 'spreadsheet' | 'image' | 'audio' | 'video' | 'code'
    | 'calendar' | 'calendar-error' | 'clock' | 'sun' | 'sun-moon' | 'palette' | 'goal' | 'user' | 'user-plus' | 'users' | 'contacts' | 'briefcase' | 'banknote' | 'workflow' | 'survey' | 'chart' | 'filter' | 'sort'
    | 'fork' | 'copy-plus' | 'share' | 'folder-plus' | 'upload' | 'home' | 'folder-move' | 'info' | 'warning' | 'error'
    | 'mail' | 'inbox' | 'spam' | 'storefront' | 'settings' | 'cloud'
    | 'reply' | 'reply-all' | 'mail-forward' | 'flag'
    | 'key' | 'shield' | 'prohibited' | 'eye' | 'eye-off' | 'incognito' | 'external' | 'import' | 'cookie'
    | 'pin' | 'pin-filled' | 'bell' | 'tasks' | 'platforms' | 'broadcast' | 'align-left' | 'align-center' | 'align-right' | 'logout' | 'google' | 'apple' | 'lock' | 'qr';
  export let size = 20;
  export let strokeWidth = 1.7;
  export let filled = false;
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill={filled ? 'currentColor' : 'none'}
  stroke="currentColor"
  stroke-width={strokeWidth}
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
  data-icon={name}
>
  {#if name === 'summary'}
    <circle cx="6" cy="8.5" r="1.5"/><circle cx="6" cy="15.5" r="1.5"/><path d="M10 8.5h9M10 15.5h9"/>
  {:else if name === 'panel'}
    <rect x="4" y="4.5" width="16" height="15" rx="3"/><path d="M15 5v14"/>
  {:else if name === 'panel-left'}
    <!-- The Workspace panel mirrored: same frame, divider on the leading edge,
         so the two drawer controls read as one pair. -->
    <rect x="4" y="4.5" width="16" height="15" rx="3"/><path d="M9 5v14"/>
  {:else if name === 'screen-record'}
    <!-- The record mark: a ring with its dot, at the same weight as the other
         permission icons. -->
    <circle cx="12" cy="12" r="8"/><circle cx="12" cy="12" r="3.6" fill="currentColor" stroke="none"/>
  {:else if name === 'computer'}
    <rect x="3" y="4" width="18" height="13" rx="2"/><path d="M12 17v4M8 21h8"/>
  {:else if name === 'devices'}
    <path d="M12 17H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v2M9 17v4M6 21h6"/><rect x="15" y="10" width="7" height="12" rx="1.5"/><path d="M18 19h1"/>
  {:else if name === 'tablet'}
    <rect x="4" y="2" width="16" height="20" rx="2"/><path d="M11 19h2"/>
  {:else if name === 'laptop'}
    <rect x="5" y="3" width="14" height="13" rx="1.5"/><path d="m5 16-3 4h20l-3-4M10 18h4"/>
  {:else if name === 'server'}
    <rect x="4" y="3" width="16" height="7" rx="1.5"/><rect x="4" y="14" width="16" height="7" rx="1.5"/><path d="M7 6.5h.01M7 17.5h.01M12 6.5h5M12 17.5h5"/>
  {:else if name === 'call'}
    <path d="M22 16.9v3a2 2 0 0 1-2.2 2 19.8 19.8 0 0 1-8.6-3.1 19.5 19.5 0 0 1-6-6 19.8 19.8 0 0 1-3.1-8.7A2 2 0 0 1 4.1 2h3a2 2 0 0 1 2 1.7c.1 1 .4 1.9.7 2.8a2 2 0 0 1-.5 2.1L8 9.9a16 16 0 0 0 6 6l1.3-1.3a2 2 0 0 1 2.1-.5c.9.3 1.8.6 2.8.7a2 2 0 0 1 1.8 2.1Z"/>
  {:else if name === 'phone'}
    <rect x="7" y="2.5" width="10" height="19" rx="2.5"/><path d="M10.5 5h3"/><circle cx="12" cy="18.6" r=".8" fill="currentColor" stroke="none"/>
  {:else if name === 'apps'}
    <!-- One application window for the aggregate app-access permission. -->
    <rect x="3.5" y="4.5" width="17" height="15" rx="2.5"/><path d="M3.5 8.5h17"/><circle cx="6.5" cy="6.5" r=".7" fill="currentColor" stroke="none"/><circle cx="9" cy="6.5" r=".7" fill="currentColor" stroke="none"/>
  {:else if name === 'platforms'}
    <!-- Four equal service tiles: an aggregate of platforms, without the
         joined-L meaning carried by the Connections icon. -->
    <rect x="3.5" y="3.5" width="7" height="7" rx="2"/><rect x="13.5" y="3.5" width="7" height="7" rx="2"/><rect x="3.5" y="13.5" width="7" height="7" rx="2"/><rect x="13.5" y="13.5" width="7" height="7" rx="2"/>
  {:else if name === 'broadcast'}
    <!-- A compact bullhorn: rear body and handle, flared cone, then three
         separate sound marks. It keeps the reference silhouette while using
         the same monochrome outline language as the rest of Polymux. -->
    <path d="M4.5 9h4.25l7.75-4.5v15L8.75 15H4.5A1.5 1.5 0 0 1 3 13.5v-3A1.5 1.5 0 0 1 4.5 9z"/>
    <path d="M6 15v4.5h3V15M16.5 8.5h1.25A1.25 1.25 0 0 1 19 9.75v4.5a1.25 1.25 0 0 1-1.25 1.25H16.5"/>
    <path d="m20 8 1.5-1M20.5 12h1.75M20 16l1.5 1"/>
  {:else if name === 'plus'}
    <path d="M12 5v14M5 12h14"/>
  {:else if name === 'align-left'}
    <path d="M5 7h14M5 11h10M5 15h14M5 19h8"/>
  {:else if name === 'align-center'}
    <path d="M5 7h14M7 11h10M5 15h14M8 19h8"/>
  {:else if name === 'align-right'}
    <path d="M5 7h14M9 11h10M5 15h14M11 19h8"/>
  {:else if name === 'logout'}
    <!-- Sign-out: a bracket door with an arrow leaving through it. -->
    <path d="M10 4H7a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h3"/><path d="m15 8 4 4-4 4M19 12h-9"/>
  {:else if name === 'bug'}
    <!-- Report-bug: a beetle in the same outline weight as the row marks beside it. -->
    <path d="m8 2 1.88 1.88"/><path d="M14.12 3.88 16 2"/>
    <path d="M9 7.13v-1a3 3 0 1 1 6 0v1"/>
    <path d="M12 20c-3.3 0-6-2.7-6-6v-3a4 4 0 0 1 4-4h4a4 4 0 0 1 4 4v3c0 3.3-2.7 6-6 6"/>
    <path d="M12 20v-9"/>
    <path d="M6.53 9C4.6 8.8 3 7.1 3 5"/><path d="M6 13H2"/>
    <path d="M3 21c0-2.1 1.7-3.9 3.8-4"/>
    <path d="M20.97 5c0 2.1-1.6 3.8-3.5 4"/><path d="M22 13h-4"/>
    <path d="M17.2 17c2.1.1 3.8 1.9 3.8 4"/>
  {:else if name === 'google'}
    <!-- The Google G reduced to its stroke skeleton: an open ring and bar,
         matching the monochrome outline language at small sizes. -->
    <path d="M21 12h-9"/><path d="M21 12A9 9 0 1 1 15.2 3.7"/>
  {:else if name === 'apple'}
    <!-- The Apple mark: body with a bite implied by the leaf notch above. -->
    <path d="M12 7.5c-1-.56-2.78-2-5-2a4.9 4.9 0 0 0-5 4.78C2 14 5 22 8 22c1.25 0 2.5-1.06 4-1.06S14.75 22 16 22c3 0 6-8 6-11.72A4.91 4.91 0 0 0 17 5.5c-2.22 0-4 1.44-5 2z"/><path d="M10 2c1 .5 2 2 2 5"/>
  {:else if name === 'close'}
    <path d="m7 7 10 10M17 7 7 17"/>
  {:else if name === 'send'}
    <path d="M7 17 17 7M17 7H9M17 7v8"/>
  {:else if name === 'mic'}
    <rect x="9" y="3" width="6" height="12" rx="3"/><path d="M5.5 11a6.5 6.5 0 0 0 13 0M12 17.5V21M9 21h6"/>
  {:else if name === 'smile'}
    <circle cx="12" cy="12" r="8.5"/><path d="M8.5 14.2a4.2 4.2 0 0 0 7 0"/><circle cx="9" cy="9.5" r=".8" fill="currentColor" stroke="none"/><circle cx="15" cy="9.5" r=".8" fill="currentColor" stroke="none"/>
  {:else if name === 'mic-off'}
    <path d="m4 4 16 16M9 6v5a3 3 0 0 0 4.7 2.5M15 10V6a3 3 0 0 0-5.7-1.3M5.5 11a6.5 6.5 0 0 0 10.7 5M12 17.5V21M9 21h6"/>
  {:else if name === 'play'}
    <path d="M9 7.4Q9 6 10.2 6.8l6.6 4.4q1.2.8 0 1.6l-6.6 4.4Q9 18 9 16.6Z" fill="currentColor" stroke="none"/>
  {:else if name === 'pause'}
    <path d="M8 6v12M16 6v12" stroke-width="2.4"/>
  {:else if name === 'speaker'}
    <path d="M5 10v4h4l5 4V6l-5 4zM17 9a4 4 0 0 1 0 6M19 6.5a8 8 0 0 1 0 11"/>
  {:else if name === 'speaker-off'}
    <path d="M5 10v4h4l5 4V6l-5 4zM17 10l4 4M21 10l-4 4"/>
  {:else if name === 'bell'}
    <path d="M6 10a6 6 0 0 1 12 0c0 3.2.7 5 1.5 6h-15C5.3 15 6 13.2 6 10M10 19.5a2.2 2.2 0 0 0 4 0"/>
  {:else if name === 'chat'}
    <path d="M5 5h14a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H9l-5 4v-4H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2z"/>
  {:else if name === 'incognito'}
    <!-- Chrome's familiar private-browsing silhouette: brimmed hat above
         round glasses, kept open so it stays legible at settings-row size. -->
    <path d="M6.5 10 8.2 4.5h7.6l1.7 5.5M4 10h16"/>
    <circle cx="8" cy="15.5" r="3"/><circle cx="16" cy="15.5" r="3"/><path d="M11 15.5h2M5 14.5l-2-1M19 14.5l2-1"/>
  {:else if name === 'waveform'}
    <path d="M4 9.8v4.4M7.2 6.4v11.2M10.4 8.7v6.6M13.6 4.8v14.4M16.8 7.3v9.4M20 10.1v3.8" stroke-width="2.5"/>
  {:else if name === 'stop'}
    <rect x="5.5" y="5.5" width="13" height="13" rx="3" fill="currentColor" stroke="none"/>
  {:else if name === 'cursor'}
    <!-- The same pointer silhouette used by Browser Use and Computer Use's
         controlled-tab badge, scaled from its native 16px geometry. -->
    <path d="M3.04536 4.45259C2.7582 3.60299 3.60299 2.7582 4.45259 3.04536L14.1828 6.33403C15.1637 6.66558 15.0872 8.08006 14.0715 8.39045L10.2994 9.54319C9.93919 9.65327 9.65327 9.93919 9.54319 10.2994L8.39046 14.0715C8.08007 15.0872 6.66558 15.1637 6.33404 14.1828L3.04536 4.45259Z" transform="scale(1.5)" fill="currentColor" stroke="none"/>
  {:else if name === 'attach'}
    <path d="m20.5 11.5-8.2 8.2a6 6 0 0 1-8.5-8.5l8.5-8.5a4 4 0 0 1 5.7 5.7l-8.5 8.5a2 2 0 0 1-2.9-2.8l7.8-7.8"/>
  {:else if name === 'options'}
    <!-- The original Polymux glyph, authored on a 20x20 grid; scaled 1.2 onto
         this component's 24x24 viewBox. The transform scales the stroke too,
         so 1.5 here renders at the same weight it did originally. -->
    <path d="M4 5h8m3 0h1M4 10h1m3 0h8M4 15h5m3 0h4M12 3v4M5 8v4M9 13v4" transform="scale(1.2)" stroke-width="1.5"/>
  {:else if name === 'verified'}
    <path d="M3.85 8.62a4 4 0 0 1 4.78-4.77 4 4 0 0 1 6.74 0 4 4 0 0 1 4.78 4.78 4 4 0 0 1 0 6.74 4 4 0 0 1-4.77 4.78 4 4 0 0 1-6.75 0 4 4 0 0 1-4.78-4.77 4 4 0 0 1 0-6.76Z"/><path d="m9 12 2 2 4-4"/>
  {:else if name === 'cloud'}
    <path d="M7.4 19.5A4.9 4.9 0 0 1 6.9 9.75 6.1 6.1 0 0 1 18.4 10.4 4.55 4.55 0 0 1 17.6 19.5z"/>
  {:else if name === 'globe'}
    <circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a15 15 0 0 1 0 18M12 3a15 15 0 0 0 0 18"/>
  {:else if name === 'cookie'}
    <path d="M12 3a9 9 0 1 0 9 9 4 4 0 0 1-5-5 4 4 0 0 1-4-4Z"/>
    <circle cx="8.5" cy="10" r="1" fill="currentColor" stroke="none"/>
    <circle cx="12" cy="15.5" r="1" fill="currentColor" stroke="none"/>
    <circle cx="7.5" cy="16" r="1" fill="currentColor" stroke="none"/>
  {:else if name === 'file'}
    <path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M15 2v5h5"/>
  {:else if name === 'task'}
    <!-- Delegation: one source node branching out to two workers. -->
    <circle cx="5" cy="12" r="2.25"/><circle cx="19" cy="6.5" r="2.25"/><circle cx="19" cy="17.5" r="2.25"/><path d="M7.25 12h5.25M12.5 6.5v11M12.5 6.5h4.25M12.5 17.5h4.25"/>
  {:else if name === 'back'}
    <path d="m15 18-6-6 6-6"/>
  {:else if name === 'forward'}
    <path d="m9 18 6-6-6-6"/>
  {:else if name === 'flag'}
    <!-- A pennant on its pole: the pole stays a hairline while the flag itself
         fills in for a message that is flagged. -->
    <path d="M6 21V4"/><path d="M6 4.8h11l-2.3 3.5 2.3 3.5H6z"/>
  {:else if name === 'reply'}
    <!-- A message turning back the way it came: the arrow lands on the left,
         and the line under it curves away to where the mail was going. -->
    <path d="M9 8 4.5 12 9 16"/><path d="M4.5 12h9a6 6 0 0 1 6 6v1"/>
  {:else if name === 'reply-all'}
    <!-- The same turn, with a second arrowhead behind it for the rest of the
         recipients. -->
    <path d="M8 8 3.5 12 8 16"/><path d="m12.5 8-2.5 2.2"/><path d="M8 12h6.5a5.5 5.5 0 0 1 5.5 5.5V19"/>
  {:else if name === 'mail-forward'}
    <!-- Reply, mirrored: the message carries on past where it stopped. -->
    <path d="m15 8 4.5 4L15 16"/><path d="M19.5 12h-9a6 6 0 0 0-6 6v1"/>
  {:else if name === 'reload'}
    <path d="M20 11a8 8 0 1 1-2.34-5.66L20 7.7M20 3v3.2q0 1.5-1.5 1.5h-3.2"/>
  {:else if name === 'download'}
    <path d="M12 3v12M7 10l5 5 5-5M5 20h14"/>
  {:else if name === 'more'}
    <circle cx="12" cy="5" r="1" fill="currentColor"/><circle cx="12" cy="12" r="1" fill="currentColor"/><circle cx="12" cy="19" r="1" fill="currentColor"/>
  {:else if name === 'ellipsis'}
    <!-- Proportioned like the old Polymux solid ellipsis: chunkier dots set a
         little tighter than a hairline three-dot glyph. -->
    <circle cx="5.4" cy="12" r="2" fill="currentColor" stroke="none"/><circle cx="12" cy="12" r="2" fill="currentColor" stroke="none"/><circle cx="18.6" cy="12" r="2" fill="currentColor" stroke="none"/>
  {:else if name === 'trash'}
    <path d="M4 7h16M9 7V4h6v3M7 7l1 14h8l1-14M10 11v6M14 11v6"/>
  {:else if name === 'expand'}
    <path d="M14 4h4.5A1.5 1.5 0 0 1 20 5.5V10M10 20H5.5A1.5 1.5 0 0 1 4 18.5V14"/>
  {:else if name === 'collapse'}
    <path d="M20 14h-4.5a1.5 1.5 0 0 0-1.5 1.5V20M4 10h4.5A1.5 1.5 0 0 0 10 8.5V4"/>
  {:else if name === 'chevron'}
    <path d="m8 10 4 4 4-4"/>
  {:else if name === 'arrow-down'}
    <path d="M12 4v16M6 14l6 6 6-6"/>
  {:else if name === 'arrow-up'}
    <path d="M12 20V4M6 10l6-6 6 6"/>
  {:else if name === 'switch'}
    <path d="M4 7h16m-4-4 4 4-4 4M20 17H4m4-4-4 4 4 4"/>
  {:else if name === 'arrow-up-right'}
    <path d="M7 17 17 7M9 7h8v8"/>
  {:else if name === 'bolt'}
    <path d="M13 2 4.5 13h7L11 22l8.5-12h-7z"/>
  {:else if name === 'document-text'}
    <path d="M7 3h7l4 4v14H7zM14 3v5h5M10 12h5M10 16h5"/>
  {:else if name === 'paper-airplane'}
    <path d="m21 3-7.5 18-3.8-7.7L2 9.5zM9.7 13.3 21 3"/>
  {:else if name === 'history'}
    <circle cx="12" cy="12" r="8.25"/><path d="M12 7v5l-3 2"/>
  {:else if name === 'archive'}
    <rect x="3" y="4" width="18" height="4" rx="1.5"/><path d="M5 8v10a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8"/><path d="M10 12h4"/>
  {:else if name === 'drive'}
    <!-- The platter lights read as dropouts at 16px unless they carry more
         weight than the drive's own outline. -->
    <rect x="3" y="4" width="18" height="7" rx="2"/><rect x="3" y="13" width="18" height="7" rx="2"/><circle cx="7" cy="7.5" r="1.15" fill="currentColor" stroke="none"/><circle cx="7" cy="16.5" r="1.15" fill="currentColor" stroke="none"/>
  {:else if name === 'new-chat'}
    <g transform="translate(1.435 1.415) scale(.9)">
      <path d="M13 3H5a3 3 0 0 0-3 3v12a3 3 0 0 0 3 3h12a3 3 0 0 0 3-3v-7"/><path d="m11 13 1.1-3.4L18.7 3a1.6 1.6 0 0 1 2.3 2.3l-6.6 6.6z"/>
    </g>
  {:else if name === 'copy'}
    <rect x="8" y="8" width="11" height="11" rx="2"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/>
  {:else if name === 'check'}
    <path d="m5 12 4 4L19 6"/>
  {:else if name === 'circle-check'}
    <circle cx="12" cy="12" r="9"/><path d="m8 12 2.7 2.7L16.5 9"/>
  {:else if name === 'circle-question'}
    <circle cx="12" cy="12" r="9"/><path d="M9.9 9.4a2.15 2.15 0 1 1 4.15.75c0 1.45-2.05 2-2.05 3.15"/><circle cx="12" cy="16.55" r=".85" fill="currentColor" stroke="none"/>
  {:else if name === 'edit'}
    <path d="M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L8 18l-4 1 1-4z"/>
  {:else if name === 'signature'}
    <!-- A written mark rather than a pen: this control chooses the reusable
         result, while the settings editor is where its words are changed. -->
    <path d="M4 17.5c2.2-5.7 4-8.5 5.4-8.5 2.2 0-.7 8.5 1.9 8.5 1.4 0 2.2-3.7 3.5-3.7 1.1 0 .2 3.7 1.7 3.7 1 0 1.8-1.3 3.5-1.3"/>
    <path d="M4 20h16"/>
  {:else if name === 'thumb-up'}
    <path d="M7 10v11H3V10h4Zm0 9h9.2a2 2 0 0 0 1.9-1.4l2.2-7A2 2 0 0 0 18.4 8H14l.7-3.1A2.4 2.4 0 0 0 10.2 3L7 10Z"/>
  {:else if name === 'thumb-down'}
    <path d="M17 14V3h4v11h-4Zm0-9H7.8a2 2 0 0 0-1.9 1.4l-2.2 7A2 2 0 0 0 5.6 16H10l-.7 3.1a2.4 2.4 0 0 0 4.5 1.9L17 14Z"/>
  {:else if name === 'search'}
    <circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/>
  {:else if name === 'zoom-in'}
    <circle cx="11" cy="11" r="7"/><path d="m20 20-4-4M11 8v6M8 11h6"/>
  {:else if name === 'zoom-out'}
    <circle cx="11" cy="11" r="7"/><path d="m20 20-4-4M8 11h6"/>
  {:else if name === 'picture-in-picture'}
    <path d="M21 9.5V6a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v10a2 2 0 0 0 2 2h5"/><rect x="12" y="12.5" width="9.5" height="7" rx="1.8"/>
  {:else if name === 'terminal'}
    <rect x="3" y="4" width="18" height="16" rx="3"/><path d="m7 9 3 3-3 3M13 15h4"/>
  {:else if name === 'wrench'}
    <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94z"/>
  {:else if name === 'book-open'}
    <path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/>
  {:else if name === 'languages'}
    <!-- Use the supplied artwork as an alpha mask so its exact silhouette
         still inherits the same colour as every other shared icon. The mark
         fills its 24 box, so at a shared size it reads larger than the padded
         outline glyphs beside it — it draws inset to land on the same optical
         size, the same trade the gear and rail tiles make. -->
    <mask id="languages-icon-mask" style="mask-type: alpha" maskUnits="userSpaceOnUse" x="0" y="0" width="24" height="24">
      <image href={languagesIcon} x="2.4" y="2.4" width="19.2" height="19.2" preserveAspectRatio="xMidYMid meet"/>
    </mask>
    <rect width="24" height="24" fill="currentColor" stroke="none" mask="url(#languages-icon-mask)"/>
  {:else if name === 'brain'}
    <path d="M12 5a3 3 0 1 0-5.997.125 4 4 0 0 0-2.526 5.77 4 4 0 0 0 .556 6.588A4 4 0 1 0 12 18Z"/>
    <path d="M12 5a3 3 0 1 1 5.997.125 4 4 0 0 1 2.526 5.77 4 4 0 0 1-.556 6.588A4 4 0 1 1 12 18Z"/>
    <path d="M12 5v14"/>
    <!-- Two folds per lobe, curving with the outline rather than across it, so
         they stay clear of the edge, the spine and each other. Drawn a little
         thinner than the outline to keep the icon calm at 14px. -->
    <g stroke-width={strokeWidth * 0.82}>
      <path d="M6 9.4c1.4.4 2.8-.1 3.5-1.2"/>
      <path d="M6.2 15.6c1.3-.3 2.7.2 3.4 1.3"/>
      <path d="M18 9.4c-1.4.4-2.8-.1-3.5-1.2"/>
      <path d="M17.8 15.6c-1.3-.3-2.7.2-3.4 1.3"/>
    </g>
  {:else if name === 'compact' || name === 'compress'}
    <!-- Lines tapering shorter with a spark at the tail: the conversation
         trimmed down and polished, not merely squeezed. -->
    <path d="M4 6h16M4 11h11M4 16h6"/>
    <path d="m17.5 14 .8 1.8 1.8.8-1.8.8-.8 1.8-.8-1.8-1.8-.8 1.8-.8z"/>
  {:else if name === 'sparkles'}
    <path d="m12 3-1.9 4.9a2.5 2.5 0 0 1-1.2 1.2L4 11l4.9 1.9a2.5 2.5 0 0 1 1.2 1.2L12 19l1.9-4.9a2.5 2.5 0 0 1 1.2-1.2L20 11l-4.9-1.9a2.5 2.5 0 0 1-1.2-1.2Z"/>
    <path d="M19 17v4M21 19h-4M5 4v3M6.5 5.5h-3"/>
  {:else if name === 'bot'}
    <path d="M12 8V4H8"/><rect width="16" height="12" x="4" y="8" rx="2"/><path d="M2 14h2M20 14h2M15 13v2M9 13v2"/>
  {:else if name === 'connections'}
    <!-- Three tiles joined as an L — one outline, shared internal walls —
         and a fourth tile detached toward the top-right on the same 2×2,
         with a 2.6-unit gutter so the float reads at 16px. -->
    <path d="M4.3 3.8h4.9Q11 3.8 11 5.6v4.9Q11 12.3 12.8 12.3h4.9Q19.5 12.3 19.5 14.1v4.9Q19.5 20.8 17.7 20.8H4.3Q2.5 20.8 2.5 19V5.6Q2.5 3.8 4.3 3.8Z"/>
    <path d="M2.5 12.3H11V20.8"/>
    <rect x="13.6" y="1.2" width="8.5" height="8.5" rx="1.8"/>
  {:else if name === 'mcp'}
    <!-- The Model Context Protocol mark exactly as its owners draw it, lifted
         from `@lobehub/icons-static-svg/icons/mcp.svg` — already a dependency,
         already on the 24 grid. It is an outlined fill rather than a stroke,
         so `strokeWidth` cannot reach it; the outline is 1.65 wide, which is
         the set's weight, and the mark reads diagonally, so at native scale it
         sits beside the 18-unit upright icons without looking larger. -->
    <g fill="currentColor" fill-rule="evenodd" stroke="none">
      <path d="M15.688 2.343a2.588 2.588 0 00-3.61 0l-9.626 9.44a.863.863 0 01-1.203 0 .823.823 0 010-1.18l9.626-9.44a4.313 4.313 0 016.016 0 4.116 4.116 0 011.204 3.54 4.3 4.3 0 013.609 1.18l.05.05a4.115 4.115 0 010 5.9l-8.706 8.537a.274.274 0 000 .393l1.788 1.754a.823.823 0 010 1.18.863.863 0 01-1.203 0l-1.788-1.753a1.92 1.92 0 010-2.754l8.706-8.538a2.47 2.47 0 000-3.54l-.05-.049a2.588 2.588 0 00-3.607-.003l-7.172 7.034-.002.002-.098.097a.863.863 0 01-1.204 0 .823.823 0 010-1.18l7.273-7.133a2.47 2.47 0 00-.003-3.537z"/>
      <path d="M14.485 4.703a.823.823 0 000-1.18.863.863 0 00-1.204 0l-7.119 6.982a4.115 4.115 0 000 5.9 4.314 4.314 0 006.016 0l7.12-6.982a.823.823 0 000-1.18.863.863 0 00-1.204 0l-7.119 6.982a2.588 2.588 0 01-3.61 0 2.47 2.47 0 010-3.54l7.12-6.982z"/>
    </g>
  {:else if name === 'link'}
    <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" transform="translate(1.68 1.68) scale(.86)" vector-effect="non-scaling-stroke"/>
  {:else if name === 'puzzle'}
    <!-- The single classic piece: knobs out of the top and right edges, sockets
         cut into the bottom and left, so it reads as an extension mark rather
         than as four interlocking pieces at 18px. -->
    <path d="M6 6h3a2.4 2.4 0 1 1 4 0h3a2 2 0 0 1 2 2v3a2.4 2.4 0 1 1 0 4v3a2 2 0 0 1-2 2h-3a2.4 2.4 0 1 0-4 0H6a2 2 0 0 1-2-2v-3a2.4 2.4 0 1 0 0-4V8a2 2 0 0 1 2-2z"/>
  {:else if name === 'grip'}
    <circle cx="9" cy="6" r="1" fill="currentColor" stroke="none"/><circle cx="15" cy="6" r="1" fill="currentColor" stroke="none"/><circle cx="9" cy="12" r="1" fill="currentColor" stroke="none"/><circle cx="15" cy="12" r="1" fill="currentColor" stroke="none"/><circle cx="9" cy="18" r="1" fill="currentColor" stroke="none"/><circle cx="15" cy="18" r="1" fill="currentColor" stroke="none"/>
  {:else if name === 'steer'}
    <path d="M4 6v4a5 5 0 0 0 5 5h10M16 12l3 3-3 3"/>
  {:else if name === 'document'}
    <rect x="5" y="3" width="14" height="18" rx="3"/><path d="M9 8h6M9 12h6M9 16h4"/>
  {:else if name === 'presentation'}
    <rect x="4" y="4" width="16" height="12" rx="2"/><path d="M8 20l4-4 4 4M12 16v4M8 9h8M8 12h5"/>
  {:else if name === 'folder'}
    <path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>
  {:else if name === 'folder-open'}
    <path d="m6 14 1.5-2.4A2 2 0 0 1 9.24 11H20a2 2 0 0 1 1.94 2l-1.54 5a2 2 0 0 1-1.95 1.2H4a2 2 0 0 1-2-1.7V7a2 2 0 0 1 2-2h3.9a2 2 0 0 1 1.69.75l.81 1a2 2 0 0 0 1.67.75H18a2 2 0 0 1 2 2V11"/>
  {:else if name === 'folder-plus'}
    <path d="M14.5 7.5H12l-2-2H5a2 2 0 0 0-2 2v9.75a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V10.5"/><path d="M18.5 2v5M16 4.5h5"/>
  {:else if name === 'memory-cited'}
    <path d="M9 20H6a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11a2 2 0 0 1 2 2v5M8 3v17"/><path d="m13 17 6-6a1.4 1.4 0 0 1 2 2l-6 6-3 1z"/>
  {:else if name === 'fork'}
    <path d="M3 12h6l11-9M13 3h7v7M9 12l11 9M14 21h6v-6"/>
  {:else if name === 'copy-plus'}
    <rect x="3" y="7" width="14" height="14" rx="3"/><path d="M7 7V6a3 3 0 0 1 3-3h8a3 3 0 0 1 3 3v8a3 3 0 0 1-3 3h-1M7 14h6M10 11v6"/>
  {:else if name === 'share'}
    <path d="M12 16V3m-4 4 4-4 4 4M5 12v8a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-8"/>
  {:else if name === 'upload'}
    <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="m17 8-5-5-5 5M12 3v12"/>
  {:else if name === 'folder-move'}
    <path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><path d="M9 13h6M13 11l2 2-2 2"/>
  {:else if name === 'info'}
    <circle cx="12" cy="12" r="9"/><path d="M12 11v5"/><circle cx="12" cy="7.75" r=".9" fill="currentColor" stroke="none"/>
  {:else if name === 'warning'}
    <!-- The triangle's broad base carries more visual weight than its point.
         Lift the complete mark by one rendered pixel at toast size so it
         reads centred beside text rather than only measuring centred. -->
    <g transform="translate(0 -0.8)"><path d="M10.3 4.2 2.9 18.1A2 2 0 0 0 4.7 21h14.6a2 2 0 0 0 1.8-2.9L13.7 4.2a2 2 0 0 0-3.4 0Z"/><path d="M12 9v4.5"/><circle cx="12" cy="17" r=".9" fill="currentColor" stroke="none"/></g>
  {:else if name === 'error'}
    <g transform="translate(0 .75)"><circle cx="12" cy="12" r="9"/><path d="M12 7.5v6"/><circle cx="12" cy="17" r=".9" fill="currentColor" stroke="none"/></g>
  {:else if name === 'home'}
    <path d="m3 9 9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><path d="M9 22V12h6v10"/>
  {:else if name === 'pdf'}
    <path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M15 2v5h5M8 14h8M8 18h5"/>
  {:else if name === 'spreadsheet'}
    <rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M3 15h18M9 3v18"/>
  {:else if name === 'image'}
    <rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="8.5" cy="8.5" r="1.8"/><path d="m20 15-4.5-4.5a2 2 0 0 0-2.8 0L5 18"/>
  {:else if name === 'audio'}
    <path d="M9 18V5l11-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="17" cy="16" r="3"/>
  {:else if name === 'video'}
    <rect x="3" y="5" width="18" height="14" rx="2"/><path d="m10 9 5 3-5 3z"/>
  {:else if name === 'code'}
    <path d="m8 9-3 3 3 3M16 9l3 3-3 3M14 5l-4 14"/>
  {:else if name === 'calendar'}
    <rect x="3" y="5" width="18" height="16" rx="3"/><path d="M8 3v4M16 3v4M3 10h18M8 14h3M8 17h6"/>
  {:else if name === 'calendar-error'}
    <rect x="3" y="5" width="18" height="16" rx="3"/><path d="M8 3v4M16 3v4M3 10h18M9.75 13.25l4.5 4.5m0-4.5-4.5 4.5"/>
  {:else if name === 'clock'}
    <circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>
  {:else if name === 'sun'}
    <circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.42 1.42M17.65 17.65l1.42 1.42M2 12h2M20 12h2M4.93 19.07l1.42-1.42M17.65 6.35l1.42-1.42"/>
  {:else if name === 'palette'}
    <path d="M12 3a9 9 0 1 0 0 18h1.2a2.3 2.3 0 0 0 1.6-3.9 1.4 1.4 0 0 1 1-2.4H18a3 3 0 0 0 3-3C21 6.9 17 3 12 3Z"/>
    <circle cx="7.5" cy="10" r=".8" fill="currentColor" stroke="none"/>
    <circle cx="10.5" cy="7" r=".8" fill="currentColor" stroke="none"/>
    <circle cx="15" cy="7.5" r=".8" fill="currentColor" stroke="none"/>
    <circle cx="7.5" cy="14.5" r=".8" fill="currentColor" stroke="none"/>
  {:else if name === 'sun-moon'}
    <!-- Light and dark as one mark: the sun sits back, the moon overlaps it from
         the right. The mask knocks a clear halo out of the sun where the moon
         crosses it, the same trick the onboarding platform stack uses with its
         background-coloured rings. -->
    <mask id="sun-moon-knockout">
      <rect x="0" y="0" width="24" height="24" fill="white"/>
      <circle cx="16.28" cy="12" r="7.29" fill="black"/>
    </mask>
    <g mask="url(#sun-moon-knockout)">
      <circle cx="7.5" cy="12" r="3.42"/>
      <path d="M7.5 5.45v1.85M7.5 16.7v1.85M0.95 12h1.85M12.2 12h1.85M2.87 7.37l1.31 1.31M12.13 7.37l-1.31 1.31M2.87 16.63l1.31-1.31M12.13 16.63l-1.31-1.31"/>
    </g>
    <path d="M16.28 5.37a4.39 4.39 0 0 0 6.63 6.63 6.63 6.63 0 1 1-6.63-6.63"/>
  {:else if name === 'goal'}
    <path d="M20.9 10.5a9 9 0 1 1-7.4-7.4"/><path d="M16.4 11.1a4.5 4.5 0 1 1-3.6-3.6"/><circle cx="12" cy="12" r="1.4"/><path d="m12 12 5-5"/><path d="M17 7V3.5L20.5 7H17Z" fill="currentColor"/>
  {:else if name === 'user'}
    <circle cx="12" cy="8" r="3.5"/><path d="M4.5 21a7.5 7.5 0 0 1 15 0"/>
  {:else if name === 'user-plus'}
    <circle cx="10" cy="8" r="3.5"/><path d="M2.5 21a7.5 7.5 0 0 1 15 0"/><path d="M18.5 2v5M16 4.5h5"/>
  {:else if name === 'users'}
    <circle cx="9" cy="8" r="3"/><path d="M3.5 20a5.5 5.5 0 0 1 11 0M15 5.5a3 3 0 0 1 0 5.8M16 15a5 5 0 0 1 4.5 5"/>
  {:else if name === 'contacts'}
    <rect x="5" y="3" width="16" height="18" rx="2.5"/><path d="M3 7h4M3 12h4M3 17h4"/><circle cx="13" cy="9" r="2.3"/><path d="M9 17a4 4 0 0 1 8 0"/>
  {:else if name === 'briefcase'}
    <rect x="3" y="7" width="18" height="13" rx="3"/><path d="M8 7V4h8v3M3 12h18M10 12v2h4v-2"/>
  {:else if name === 'banknote'}
    <rect x="3" y="6" width="18" height="12" rx="2"/><circle cx="12" cy="12" r="3"/><path d="M7 9H6v1M17 15h1v-1"/>
  {:else if name === 'workflow'}
    <rect x="3" y="9.5" width="5" height="5" rx="1.5"/><rect x="16" y="3.5" width="5" height="5" rx="1.5"/><rect x="16" y="15.5" width="5" height="5" rx="1.5"/><path d="M8 12h3.5a1.5 1.5 0 0 0 1.5-1.5v-2A2 2 0 0 1 15 6M8 12h3.5a1.5 1.5 0 0 1 1.5 1.5v2a2 2 0 0 0 2 2h1"/>
  {:else if name === 'survey'}
    <path d="M4 4h16v12H8l-4 4z"/><path d="M8 8h8M8 12h5"/>
  {:else if name === 'chart'}
    <path d="M3 20h18"/><rect x="5" y="11" width="3.6" height="9" rx="1"/><rect x="10.2" y="6" width="3.6" height="14" rx="1"/><rect x="15.4" y="9" width="3.6" height="11" rx="1"/>
  {:else if name === 'mail'}
    <rect x="3" y="5" width="18" height="14" rx="3"/><path d="m4.5 8.2 6.6 4.6a1.6 1.6 0 0 0 1.8 0l6.6-4.6"/>
  {:else if name === 'inbox'}
    <path d="M22 12h-6l-2 3h-4l-2-3H2"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/>
  {:else if name === 'spam'}
    <!-- The archive tray with a cross through it: junk is filed away and
         struck out, which reads apart from the bin beside it. -->
    <rect x="3" y="4" width="18" height="4" rx="1.5"/><path d="M5 8v10a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8"/><path d="m10 12 4 4m0-4-4 4"/>
  {:else if name === 'storefront'}
    <path d="M4 10v10h16V10M3 10l2-6h14l2 6"/><path d="M3 10a3 3 0 0 0 5 2 3 3 0 0 0 4 0 3 3 0 0 0 4 0 3 3 0 0 0 5-2M9 20v-5h6v5"/>
  {:else if name === 'settings'}
    <path d="M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33h.09a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51h.09a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82v.09a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z"/>
  {:else if name === 'filter'}
    <path d="M4 6h16M7 12h10M10 18h4"/>
  {:else if name === 'sort'}
    <path d="M8 6h10M8 12h7M8 18h4M4 5v14M2 17l2 2 2-2"/>
  {:else if name === 'key'}
    <circle cx="8" cy="15" r="3.5"/><path d="m10.6 12.4 7.4-7.4M15.5 7.5l2 2M18 5l2 2"/>
  {:else if name === 'lock'}
    <rect x="5" y="11" width="14" height="10" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>
  {:else if name === 'shield'}
    <path d="M12 3.5 5.5 6v5.6c0 3.9 2.7 6.9 6.5 8.4 3.8-1.5 6.5-4.5 6.5-8.4V6z"/>
  {:else if name === 'prohibited'}
    <circle cx="12" cy="12" r="8.5"/><path d="m6 18 12-12"/>
  {:else if name === 'eye'}
    <path d="M2.5 12S6 6.5 12 6.5 21.5 12 21.5 12 18 17.5 12 17.5 2.5 12 2.5 12z"/><circle cx="12" cy="12" r="2.8"/>
  {:else if name === 'eye-off'}
    <!-- The same eye, struck through: the shown and hidden states are one
         stroke apart, so the button does not appear to change icon families. -->
    <path d="M10.7 6.7A10 10 0 0 1 12 6.5c6 0 9.5 5.5 9.5 5.5a17.4 17.4 0 0 1-3.5 3.9M7.1 8.4A17.2 17.2 0 0 0 2.5 12S6 17.5 12 17.5c1 0 2-.2 2.9-.5"/><path d="M9.9 9.9a2.8 2.8 0 0 0 4.2 4.2"/><path d="m4 4 16 16"/>
  {:else if name === 'pin' || name === 'pin-filled'}
    <!-- Both states share the same silhouette; a solid head marks a pinned item. -->
    <g transform="rotate(45 12 12)">
      <path d="M9 3.5h6l-.7 5.2 3.2 3.1H6.5l3.2-3.1z" fill={name === 'pin-filled' || filled ? 'currentColor' : 'none'}/><path d="M12 11.8V20.5"/>
    </g>
  {:else if name === 'import'}
    <path d="M12 4v9M12 13l3.5-3.5M12 13 8.5 9.5"/><path d="M5 15.5V18a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-2.5"/>
  {:else if name === 'external'}
    <!-- The arrow leaving a frame, matching the 'link' pair beside it. -->
    <path d="M14 4.5h5.5V10M19.5 4.5 11 13"/><path d="M18 14.5V18a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h3.5"/>
  {:else if name === 'qr'}
    <rect x="3" y="3" width="6" height="6" rx="1"/><rect x="15" y="3" width="6" height="6" rx="1"/><rect x="3" y="15" width="6" height="6" rx="1"/><path d="M15 15h3v3h3M15 21h3M21 12v3M12 3v3M3 12h3M12 9v6"/>
  {:else if name === 'tasks'}
    <rect x="4" y="3" width="16" height="18" rx="2.5"/><rect x="7.5" y="1.5" width="9" height="3" rx="1.5"/><path d="m9.5 11.5 2 2 3.5-4"/>
  {/if}
</svg>
