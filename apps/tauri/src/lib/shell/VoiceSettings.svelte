<script lang="ts">
  import { onMount } from 'svelte';
  import type { Provider, Settings, SettingsPatch } from '../api/contract';
  import type { Session } from '../session.svelte';

  let { session, providers }: { session: Session; providers: Provider[] } = $props();
  let settings = $state<Settings>();
  let issue = $state('');
  let saving = $state(false);

  const voices: Record<string, string[]> = {
    gemini: ['Aoede', 'Kore', 'Puck', 'Charon', 'Fenrir', 'Leda', 'Orus', 'Zephyr'],
    openai: ['marin', 'cedar', 'alloy', 'ash', 'ballad', 'coral', 'echo', 'sage', 'shimmer', 'verse'],
  };
  const languages = ['en', 'es', 'fr', 'de', 'it', 'pt', 'nl', 'pl', 'ru', 'tr', 'ar', 'hi', 'id', 'ja', 'ko', 'th', 'vi', 'zh'];
  const live = $derived(settings?.voiceMode === 'live');
  const liveModels = $derived.by(() => {
    const current = settings;
    if (!current) return [];
    const defaults = current.liveProvider === 'openai' ? ['gpt-realtime', 'gpt-realtime-mini'] : ['gemini-3.8-live'];
    const available = providers.filter((provider) => provider.enabled && provider.type === current.liveProvider)
      .flatMap((provider) => provider.models).filter((model) => model.enabled && model.tags.includes('live')).map((model) => model.id);
    return [...new Set([...defaults, ...available, current.liveModel])].filter(Boolean).sort();
  });
  const hasKey = $derived(!!settings && settings.speechKeys.includes(settings.liveProvider));

  onMount(() => { void load(); });
  $effect(() => { if (session.online && !settings) void load(); });

  async function load() {
    try {
      const value = await session.client.settings();
      if (!saving) settings = value;
    } catch { /* Settings remain unavailable until the next connection. */ }
  }

  async function save(patch: SettingsPatch) {
    if (!settings || saving) return;
    const previous = settings;
    settings = { ...settings, ...patch };
    saving = true;
    issue = '';
    try { settings = await session.client.updateSettings(patch); }
    catch { settings = previous; issue = 'Couldn’t save voice settings.'; }
    finally { saving = false; }
  }

  function chooseProvider(provider: string) {
    void save({ liveProvider: provider, liveModel: provider === 'openai' ? 'gpt-realtime' : 'gemini-3.8-live', liveVoice: voices[provider]?.[0] });
  }

  function languageName(code: string) {
    try { return new Intl.DisplayNames(undefined, { type: 'language' }).of(code) ?? code; }
    catch { return code; }
  }
</script>

{#if settings}
<section class="voice">
  <div class="heading"><span>Voice</span></div>
  <div class="card">
    <label class="row"><span>Mode</span><select aria-label="Voice mode" disabled={saving} value={settings.voiceMode} onchange={(event) => void save({ voiceMode: event.currentTarget.value })}><option value="pipeline">Take turns</option><option value="live">Live conversation</option></select></label>
    <p class="note">{live ? 'Talk naturally and interrupt replies.' : 'Speak, then wait for a reply.'}</p>
    {#if live}
      <div class="divider"></div>
      <label class="row"><span>Provider</span><select aria-label="Voice provider" disabled={saving} value={settings.liveProvider} onchange={(event) => chooseProvider(event.currentTarget.value)}><option value="gemini">Gemini Live</option><option value="openai">OpenAI Realtime</option></select></label>
      <label class="row"><span>Model</span><select aria-label="Voice model" disabled={saving} value={settings.liveModel} onchange={(event) => void save({ liveModel: event.currentTarget.value })}>{#each liveModels as model (model)}<option value={model}>{model}</option>{/each}</select></label>
      <label class="row"><span>Voice</span><select aria-label="Voice" disabled={saving} value={settings.liveVoice} onchange={(event) => void save({ liveVoice: event.currentTarget.value })}>{#each voices[settings.liveProvider] ?? [] as voice (voice)}<option value={voice}>{voice.charAt(0).toUpperCase() + voice.slice(1)}</option>{/each}</select></label>
      {#if !hasKey}<p class="note">No {settings.liveProvider === 'openai' ? 'OpenAI' : 'Gemini'} provider yet.</p>{/if}
    {/if}
    <div class="divider"></div>
    <label class="row"><span>Language</span><select aria-label="Voice language" disabled={saving} value={settings.voiceLanguage} onchange={(event) => void save({ voiceLanguage: event.currentTarget.value })}><option value="">System</option>{#each languages as code (code)}<option value={code}>{languageName(code)}</option>{/each}</select></label>
  </div>
  {#if issue}<p class="issue" role="status">{issue}</p>{/if}
</section>
{/if}

<style>
  .voice { margin-bottom: 22px; }
  .heading { font-size: 13px; font-weight: 600; margin: 0 0 10px; }
  .card { border-radius: 10px; background: var(--sidebar-bg); padding: 4px 10px; }
  .row { display: flex; align-items: center; justify-content: space-between; gap: 16px; min-height: 36px; font-size: 13px; }
  .row + .row, .divider + .row { border-top: 1px solid var(--hairline); }
  select { max-width: 190px; border: 0; background: transparent; color: var(--text-muted); font: inherit; text-align: right; outline: none; }
  select:focus-visible { color: var(--text); }
  .divider { height: 1px; background: var(--hairline); }
  .note, .issue { margin: 4px 0 6px; color: var(--text-muted); font-size: 11px; line-height: 1.35; }
  .issue { margin: 8px 2px 0; }
</style>
