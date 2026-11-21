<script module lang="ts">
  export type AppId = 'chrome' | 'slack' | 'figma' | 'notion' | 'mail' | 'calendar';
</script>

<script lang="ts">
  import chrome from '../assets/apps/chrome.webp?no-inline';
  import slack from '../assets/apps/slack.webp?no-inline';
  import figma from '../assets/apps/figma.webp?no-inline';
  import notion from '../assets/apps/notion.webp?no-inline';
  import mail from '../assets/apps/mail.webp?no-inline';
  import calendar from '../assets/apps/calendar.webp?no-inline';

  let {id, size}: {id: AppId; size?: number} = $props();

  // Official app icons: store artwork for third-party apps, macOS system icons for Mail and Calendar.
  const icons: Record<AppId, string> = {chrome, slack, figma, notion, mail, calendar};
</script>

<img class="app-icon" src={icons[id]} alt="" width={size ?? 64} height={size ?? 64} decoding="async" />

<style>
  .app-icon { display: block; max-width: none; border-radius: 22.5%; }
</style>
