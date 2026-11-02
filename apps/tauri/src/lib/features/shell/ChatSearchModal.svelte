<script lang="ts">
  import {tick} from 'svelte';
  import type {ChatEntry} from './ChatDrawer.svelte';
  import {chatSearchSnippets, type ChatSearchSnippet} from './chatSearchSnippets';
  import Icon from '../../shared/components/Icon.svelte';
  import TeamRoleLabel from '../team/TeamRoleLabel.svelte';
  import BloubAvatar from '../team/BloubAvatar.svelte';
  import {t} from '../../../i18n';

  let {
    chats = [],
    onOpen = () => {},
    onClose = () => {},
  }: {
    chats?: ChatEntry[];
    onOpen?: (id: string, messageId?: string) => void;
    onClose?: () => void;
  } = $props();

  type ChatSearchHit = ChatEntry & {snippets: ChatSearchSnippet[]};

  let query = $state('');
  let cursor = $state(0);
  let list = $state<HTMLElement | undefined>();
  let atTop = $state(true);
  let atBottom = $state(true);

  const results = $derived(filter(chats, query));
  const targets = $derived(results.flatMap((chat) => [
    {chatId: chat.id, messageId: undefined as string | undefined},
    ...chat.snippets.map((snippet) => ({chatId: chat.id, messageId: snippet.messageId})),
  ]));
  const activeIndex = $derived(targets.length ? Math.min(cursor, targets.length - 1) : 0);
  function targetIndex(chatId: string, messageId?: string): number {
    return targets.findIndex((target) => target.chatId === chatId && target.messageId === messageId);
  }
  const searchLabel = $derived($t('chats.search'));
  const emptyLabel = $derived(
    query.trim() ? $t('chats.noMatches', {query: query.trim()}) : $t('chats.none'),
  );

  function filter(entries: ChatEntry[], text: string): ChatSearchHit[] {
    const needle = text.trim().toLowerCase();
    const matched = needle
      ? entries.filter((chat) => `${chat.title} ${chat.keywords ?? ''}`.toLowerCase().includes(needle))
      : entries.slice();
    return matched.sort((a, b) => b.updatedAt - a.updatedAt).slice(0, 50).map((chat) => ({
      ...chat,
      snippets: chatSearchSnippets(chat.searchMessages, text),
    }));
  }

  function measureEdges(node = list): void {
    if (!node) return;
    atTop = node.scrollTop <= 1;
    atBottom = node.scrollHeight - node.scrollTop - node.clientHeight <= 1;
  }

  function focusInput(node: HTMLInputElement): void {
    node.focus();
  }

  function bindList(node: HTMLElement): () => void {
    list = node;
    $effect(() => {
      void results;
      void tick().then(() => measureEdges(node));
    });
    return () => {
      if (list === node) list = undefined;
    };
  }

  function move(delta: number): void {
    if (!targets.length) return;
    cursor = (activeIndex + delta + targets.length) % targets.length;
    void tick().then(() => {
      list?.querySelector('[aria-selected="true"]')?.scrollIntoView({block: 'nearest'});
      measureEdges();
    });
  }

  function choose(id: string, messageId?: string): void {
    onOpen(id, messageId);
    onClose();
  }

  function keydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault();
      onClose();
    } else if (event.key === 'ArrowDown') {
      event.preventDefault();
      move(1);
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      move(-1);
    } else if (event.key === 'Enter' && !(event.target instanceof HTMLButtonElement) && targets[activeIndex]) {
      event.preventDefault();
      choose(targets[activeIndex].chatId, targets[activeIndex].messageId);
    }
  }
</script>

<svelte:window onkeydown={keydown}/>

<div
  class="chat-search-backdrop"
  role="presentation"
  onclick={(event) => { if (event.target === event.currentTarget) onClose(); }}
>
  <div class="chat-search" role="dialog" aria-modal="true" aria-label={searchLabel}>
    <div class="chat-search-field">
      <Icon name="search" size={16}/>
      <input
        bind:value={query}
        oninput={() => cursor = 0}
        {@attach focusInput}
        type="text"
        placeholder={searchLabel}
        aria-label={searchLabel}
        aria-controls="chat-search-results"
        autocomplete="off"
        spellcheck="false"
      />
    </div>
    {#if results.length}
      <ul
        {@attach bindList}
        class:at-top={atTop}
        class:at-bottom={atBottom}
        id="chat-search-results"
        role="listbox"
        aria-label={$t('common.results')}
        onscroll={() => measureEdges()}
      >
        {#each results as chat (chat.id)}
          <li role="presentation" class:bot-result={chat.avatar}>
            <div role="option" aria-selected={targetIndex(chat.id) === activeIndex} aria-label={chat.title}>
              <button type="button" data-tooltip="none" onmouseenter={() => cursor = targetIndex(chat.id)} onclick={() => choose(chat.id)}>
                {#if chat.avatar}
                  <span class="chat-search-identity">
                    <BloubAvatar avatar={chat.avatar} size={22} animated={false} centerSilhouette label={`${chat.title} avatar`}/>
                    <span class="chat-search-title">{chat.title}</span>
                    <TeamRoleLabel role={chat.role ?? ''} raised={targetIndex(chat.id) === activeIndex}/>
                  </span>
                {:else}
                  <span class="chat-search-title">{chat.title}</span>
                {/if}
              </button>
            </div>
            {#each chat.snippets as snippet (snippet.messageId)}
              <div role="option" aria-selected={targetIndex(chat.id, snippet.messageId) === activeIndex} aria-label={`${snippet.before}${snippet.match}${snippet.after}`}>
                <button type="button" class="chat-search-match" onmouseenter={() => cursor = targetIndex(chat.id, snippet.messageId)} onclick={() => choose(chat.id, snippet.messageId)}>
                  <span class="chat-search-snippet">{snippet.before}<mark>{snippet.match}</mark>{snippet.after}</span>
                </button>
              </div>
            {/each}
          </li>
        {/each}
      </ul>
    {:else}
      <p class="chat-search-empty">
        {emptyLabel}
      </p>
    {/if}
  </div>
</div>

<style>
  .chat-search-backdrop{position:fixed;z-index:1000;inset:0;display:grid;justify-items:center;align-items:center;padding:24px;background:rgba(20,20,20,.24);backdrop-filter:blur(5px);animation:chat-search-backdrop-in .16s ease-out}
  .chat-search{width:min(520px,100%);height:min(348px,68vh);display:flex;flex-direction:column;overflow:hidden;border:1px solid var(--neutral-200);border-radius:16px;background:var(--app-bg);box-shadow:0 24px 80px rgba(0,0,0,.3);animation:chat-search-in .2s cubic-bezier(.22,1,.36,1)}
  .chat-search-field{flex:none;display:flex;align-items:center;gap:9px;padding:13px 16px;border-bottom:1px solid var(--neutral-200);color:var(--neutral-500)}
  .chat-search-field input{min-width:0;flex:1;border:0;padding:0;background:transparent;color:var(--neutral-950);font-family:inherit;font-size:14px}
  .chat-search-field input:focus{outline:0}
  .chat-search-field input::placeholder{color:var(--neutral-400)}
  .chat-search ul{--mask-top:transparent;--mask-bottom:transparent;min-height:0;flex:1;overflow-y:auto;margin:0;padding:6px;list-style:none;scrollbar-width:none;-webkit-mask-image:linear-gradient(to bottom,var(--mask-top),#000 18px,#000 calc(100% - 18px),var(--mask-bottom));mask-image:linear-gradient(to bottom,var(--mask-top),#000 18px,#000 calc(100% - 18px),var(--mask-bottom))}
  .chat-search ul::-webkit-scrollbar{display:none}
  .chat-search ul.at-top{--mask-top:#000}
  .chat-search ul.at-bottom{--mask-bottom:#000}
  .chat-search li button{width:100%;display:flex;flex-direction:column;align-items:stretch;justify-content:center;gap:4px;min-height:34px;border:0;border-radius:9px;padding:7px 10px;background:transparent;color:var(--neutral-800);cursor:pointer;text-align:left;font-family:inherit;font-size:13px}
  .chat-search-title{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .chat-search-identity{min-width:0;display:flex;align-items:center;gap:8px}
  .chat-search-identity .chat-search-title{flex:0 1 auto;max-width:50%}
  .chat-search .bot-result .chat-search-match{padding-left:40px}
  .chat-search li .chat-search-match{min-height:28px;padding:5px 10px 5px 22px}
  .chat-search li .chat-search-match .chat-search-snippet{margin:0}
  .chat-search-snippet{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;margin:0 0 1px 12px;color:var(--neutral-700);font-size:11.5px;font-weight:450;line-height:1.35}
  .chat-search-snippet mark{margin:0;padding:0 .12em;border-radius:3px;background:color-mix(in srgb,var(--neutral-950) 14%,transparent);color:var(--neutral-950);font:inherit;font-weight:560}
  .chat-search [role="option"][aria-selected="true"] button,.chat-search li button:focus-visible{outline:0;background:var(--neutral-100);color:var(--neutral-950)}
  .chat-search [role="option"][aria-selected="true"] .chat-search-snippet,.chat-search li button:focus-visible .chat-search-snippet{color:var(--neutral-800)}
  .chat-search-empty{min-height:0;flex:1;display:flex;align-items:center;justify-content:center;margin:0;padding:26px 16px;color:var(--neutral-400);text-align:center;font-size:12.5px}
  @keyframes chat-search-backdrop-in{from{opacity:0}}
  @keyframes chat-search-in{from{opacity:0;transform:translateY(-8px) scale(.99)}}
</style>
