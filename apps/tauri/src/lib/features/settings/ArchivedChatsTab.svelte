<script lang="ts">
  import {onMount} from 'svelte';
  import type {ConversationDto, PolymuxApi} from '@polymux/protocol';
  import {readableError} from '../../shared/errors';
  import {scrollFade} from '../../shared/scrollFade';
  import Icon from '../../shared/components/Icon.svelte';
  import {activeLocale, t} from '../../../i18n';
  import {clockTime} from '../../shared/displayTime';

  let {api, onChatsChanged = () => {}}: {
    api: PolymuxApi;
    onChatsChanged?: () => void;
  } = $props();

  let chats = $state.raw<ConversationDto[]>([]);
  let query = $state('');
  let loading = $state(true);
  let error = $state('');
  let busyId = $state('');
  let confirmingId = $state('');

  const needle = $derived(query.trim().toLocaleLowerCase());
  const visible = $derived(
    needle ? chats.filter((chat) => chat.title.toLocaleLowerCase().includes(needle)) : chats,
  );

  onMount(() => {
    void load();
    const refresh = () => { void load(true); };
    window.addEventListener('polymux-chats-changed', refresh);
    return () => window.removeEventListener('polymux-chats-changed', refresh);
  });

  async function load(quiet = false): Promise<void> {
    error = '';
    if (!quiet) loading = true;
    try {
      chats = await api.conversations.listArchived();
    } catch (cause) {
      error = readableError(cause);
    } finally {
      loading = false;
    }
  }

  async function unarchive(chat: ConversationDto): Promise<void> {
    confirmingId = '';
    busyId = chat.id;
    error = '';
    try {
      await api.conversations.unarchive(chat.id);
      chats = chats.filter((item) => item.id !== chat.id);
      onChatsChanged();
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busyId = '';
    }
  }

  async function remove(chat: ConversationDto): Promise<void> {
    if (confirmingId !== chat.id) {
      confirmingId = chat.id;
      return;
    }
    busyId = chat.id;
    error = '';
    try {
      await api.conversations.remove(chat.id);
      chats = chats.filter((item) => item.id !== chat.id);
      confirmingId = '';
      onChatsChanged();
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busyId = '';
    }
  }

  /** A row's stamp: the shared 12-hour clock in the interface language, widening
   * from a time to a weekday to a date. */
  function formatArchived(iso: string | null): string {
    const when = new Date(iso ?? '');
    if (Number.isNaN(when.getTime())) return '';
    const now = new Date();
    if (when.toDateString() === now.toDateString()) return clockTime(when);
    const days = (now.getTime() - when.getTime()) / 86_400_000;
    if (days < 7) {
      return when.toLocaleDateString(activeLocale(), {
        weekday: 'short',
        hour: 'numeric',
        minute: '2-digit',
        hour12: true,
      });
    }
    return when.toLocaleDateString(activeLocale(), {
      month: 'short',
      day: 'numeric',
      year: when.getFullYear() === now.getFullYear() ? undefined : 'numeric',
    });
  }
</script>

<div class="archived-chats" role="tabpanel">
  {#if error}<p class="archived-error" role="alert">{error}</p>{/if}
  <div class="archived-search">
    <Icon name="search" size={15}/>
    <input bind:value={query} type="search" placeholder={$t('settings.searchArchivedChats')} aria-label={$t('settings.searchArchivedChats')} spellcheck="false"/>
    {#if query}<button type="button" class="search-clear" aria-label={$t('settings.clear')} data-tooltip-label={$t('settings.clear')} onclick={() => query = ''}><Icon name="close" size={13} strokeWidth={1.7}/></button>{/if}
  </div>
  <ul class="archived-list" class:empty={!visible.length} use:scrollFade={visible.length}>
    {#each visible as chat (chat.id)}
      <li>
        <span class="archived-copy">
          <strong>{chat.title.trim() || 'New chat'}</strong>
          <small>{formatArchived(chat.archivedAt ?? chat.updatedAt)}</small>
        </span>
        <span class="archived-actions">
          {#if confirmingId === chat.id}
            <button type="button" disabled={busyId === chat.id} onclick={() => confirmingId = ''}>{$t('common.cancel')}</button>
            <button type="button" class="danger" disabled={busyId === chat.id} aria-label={`${$t('settings.deleteArchivedChat')} ${chat.title}`} onclick={() => void remove(chat)}>{$t('settings.deleteArchivedChat')}</button>
          {:else}
            <button type="button" disabled={busyId === chat.id} aria-label={`${$t('chats.unarchive')} ${chat.title}`} onclick={() => void unarchive(chat)}>{$t('chats.unarchive')}</button>
            <button type="button" class="danger" disabled={busyId === chat.id} aria-label={`${$t('common.delete')} ${chat.title}`} onclick={() => void remove(chat)}>{$t('common.delete')}</button>
          {/if}
        </span>
      </li>
    {:else}
      <li class="archived-empty">{loading ? '' : needle ? $t('settings.noArchivedChatsMatch', {query: query.trim()}) : $t('settings.noArchivedChats')}</li>
    {/each}
  </ul>
</div>

<style>
  .archived-chats{flex:1;min-height:0;display:flex;flex-direction:column;padding:2px var(--options-detail-edge) 20px calc(var(--options-content-edge) + var(--options-tab-inline))}
  .archived-error{margin:0 0 10px;padding:7px 10px;border-radius:8px;background:var(--neutral-100);color:var(--neutral-700);font-size:12px}
  .archived-search{display:flex;align-items:center;gap:7px;height:30px;flex:none;padding:0 10px;border:1px solid var(--neutral-200);border-radius:9px;background:var(--input-surface);color:var(--neutral-500)}
  .archived-search:focus-within{border-color:var(--neutral-400);background:var(--prompt-surface-active)}
  .archived-search input{-webkit-appearance:none;appearance:none;min-width:0;flex:1;border:0;padding:0;background:transparent;color:var(--neutral-950);outline:none;font-size:12.5px}
  .archived-search input::-webkit-search-cancel-button{-webkit-appearance:none;appearance:none}
  .search-clear{appearance:none;width:13px;height:20px;display:grid;flex:none;place-items:center;border:0;padding:0;background:transparent;color:var(--neutral-400);cursor:pointer}
  .search-clear:hover,.search-clear:focus-visible{outline:0;color:var(--neutral-800)}
  .archived-list{flex:1;min-height:0;overflow-y:auto;margin:8px 0 0;padding:0;list-style:none;scrollbar-width:none}
  .archived-list::-webkit-scrollbar{display:none}
  .archived-list.empty{display:flex;align-items:center;justify-content:center;-webkit-mask-image:none;mask-image:none}
  .archived-list li{display:flex;align-items:center;gap:12px;min-height:52px;border-bottom:1px solid var(--neutral-200)}
  .archived-list li:last-child{border-bottom:0}
  .archived-copy{min-width:0;flex:1;display:flex;flex-direction:column;gap:2px}
  .archived-copy strong{overflow:hidden;color:var(--neutral-950);text-overflow:ellipsis;white-space:nowrap;font-size:12.5px;font-weight:570}
  .archived-copy small{overflow:hidden;color:var(--neutral-500);text-overflow:ellipsis;white-space:nowrap;font-size:10.5px}
  .archived-actions{flex:none;display:flex;align-items:center;gap:10px}
  .archived-actions button{min-height:28px;padding:0;border:0;background:transparent;color:var(--neutral-500);cursor:pointer;font:inherit;font-size:11px;font-weight:550}
  .archived-actions button:hover,.archived-actions button:focus-visible{outline:0;color:var(--neutral-950)}
  .archived-actions button.danger:hover,.archived-actions button.danger:focus-visible{color:var(--danger-500)}
  .archived-actions button:disabled{cursor:default;opacity:.5}
  .archived-empty{justify-content:center;border:0;color:var(--neutral-400);text-align:center;font-size:12px}
</style>
