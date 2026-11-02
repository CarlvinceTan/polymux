<script lang="ts">
  import {tick} from 'svelte';
  import {SUPPORTED_LANGUAGES, type AccountStatusDto} from '@polymux/protocol';
  import Icon from '../../shared/components/Icon.svelte';
  import Menu from '../../shared/components/Menu.svelte';
  import MenuItem from '../../shared/components/MenuItem.svelte';
  import {polymuxApi} from '../../api/polymux';
  import {scrollFade} from '../../shared/scrollFade';
  import {applyLanguage, t} from '../../../i18n';

  const api = polymuxApi();
  const REPORT_BUG_URL = 'https://github.com/CarlvinceTan/Polymux/issues';
  const CONTACT_URL = 'mailto:carlvince@polymux.com';
  const MENU_GROUP = 'account-profile';

  let {
    status = null,
    onSignIn = () => {},
    onSignOut = () => {},
    onDocumentation,
  }: {
    status?: AccountStatusDto | null;
    onSignIn?: () => void;
    onSignOut?: () => void;
    onDocumentation: () => void;
  } = $props();

  let open = $state(false);
  let languageOpen = $state(false);
  let switchOpen = $state(false);
  let languageRow = $state<HTMLButtonElement | null>(null);
  let switchRow = $state<HTMLButtonElement | null>(null);
  let slotEl = $state<HTMLDivElement | undefined>();
  let languageOpensLeft = $state(false);
  let switchOpensLeft = $state(false);
  let avatarFailed = $state(false);
  let language = $state('system');
  let languageQuery = $state('');
  let languageSearchEl = $state<HTMLInputElement | undefined>();

  const languageMatches = $derived.by(() => {
    const query = languageQuery.trim().toLocaleLowerCase();
    if (!query) return SUPPORTED_LANGUAGES;
    return SUPPORTED_LANGUAGES.filter((option) =>
      option.label.toLocaleLowerCase().includes(query)
      || option.value.toLocaleLowerCase().includes(query),
    );
  });

  const profile = $derived(status?.profile ?? null);
  const others = $derived(status?.accounts ?? []);
  const avatarUrl = $derived(profile?.avatarUrl || null);
  const initials = $derived((profile?.name || profile?.email || 'U').trim().charAt(0).toUpperCase());
  const footer = $derived((slotEl?.closest('.chat-drawer-footer') as HTMLElement | null) ?? null);
  const drawer = $derived((slotEl?.closest('.chat-drawer') as HTMLElement | null) ?? null);

  async function loadLanguage(): Promise<void> {
    try {
      const general = await api.general.get();
      language = general.language ?? 'system';
    } catch {
      // Keep the last known choice; the menu still opens.
    }
  }

  function closeSubmenus(): void {
    languageOpen = false;
    switchOpen = false;
    languageQuery = '';
  }

  async function onProfileClick(): Promise<void> {
    if (!status?.signedIn) {
      if (status?.available !== false) onSignIn();
      return;
    }
    if (open) {
      open = false;
      closeSubmenus();
      return;
    }
    await loadLanguage();
    closeSubmenus();
    open = true;
  }

  function choose(action: 'add' | 'signout' | 'docs' | 'bug' | 'contact'): void {
    open = false;
    closeSubmenus();
    if (action === 'add') onSignIn();
    else if (action === 'signout') onSignOut();
    else if (action === 'docs') onDocumentation();
    else if (action === 'bug') void api.browser.openExternal(REPORT_BUG_URL);
    else void api.browser.openExternal(CONTACT_URL);
  }

  async function switchTo(userId: string): Promise<void> {
    open = false;
    closeSubmenus();
    await api.account.switchTo(userId);
  }

  async function toggleLanguage(event: MouseEvent): Promise<void> {
    event.stopPropagation();
    languageOpen = !languageOpen;
    if (languageOpen) {
      switchOpen = false;
      languageQuery = '';
      // The search takes the caret so typing filters straight away.
      await tick();
      languageSearchEl?.focus();
    } else {
      languageQuery = '';
    }
  }

  function toggleSwitch(event: MouseEvent): void {
    event.stopPropagation();
    switchOpen = !switchOpen;
    if (switchOpen) {
      languageOpen = false;
      languageQuery = '';
    }
  }

  async function setLanguage(value: string): Promise<void> {
    try {
      const general = await api.general.update({language: value});
      applyLanguage(general.language);
      language = general.language;
    } catch {
      // Leave the radio on the last applied value.
    }
  }
</script>

<Menu
  bind:open
  label={$t('account.menu')}
  placement="above"
  width="full"
  inset
  match={footer}
  insetParent={drawer}
  group={MENU_GROUP}
  listClass="chat-drawer-profile-menu"
  onClose={closeSubmenus}
>
  {#snippet trigger()}
    <div bind:this={slotEl} class="chat-drawer-profile-slot">
      <button
        type="button"
        class="chat-drawer-profile"
        aria-haspopup={status?.signedIn ? 'menu' : undefined}
        aria-expanded={status?.signedIn ? open : undefined}
        onclick={() => void onProfileClick()}
      >
        {#if avatarUrl && !avatarFailed}
          <img class="chat-drawer-profile-avatar" src={avatarUrl} referrerpolicy="no-referrer" alt="" onload={() => avatarFailed = false} onerror={() => avatarFailed = true}/>
        {:else if profile}
          <span class="chat-drawer-profile-avatar chat-drawer-profile-fallback" aria-hidden="true">{initials}</span>
        {:else}
          <span class="chat-drawer-profile-avatar chat-drawer-profile-fallback" aria-hidden="true"><Icon name="user" size={12}/></span>
        {/if}
        <span class="chat-drawer-profile-name">{status?.signedIn ? (profile?.name || profile?.email) : $t('account.signIn')}</span>
      </button>
    </div>
  {/snippet}

  {#if profile?.email}
    <div class="menu-heading">{profile.email}</div>
  {/if}
  <div class="menu-separator" role="separator"></div>
  <MenuItem
    bind:el={languageRow}
    icon="languages"
    chevron
    chevronLeft={languageOpensLeft}
    active={languageOpen}
    onclick={toggleLanguage}
  >{$t('settings.language')}</MenuItem>
  <MenuItem icon="book-open" onclick={() => choose('docs')}>{$t('account.documentation')}</MenuItem>
  <MenuItem icon="bug" onclick={() => choose('bug')}>{$t('account.reportBug')}</MenuItem>
  <MenuItem icon="mail" onclick={() => choose('contact')}>{$t('account.contactUs')}</MenuItem>
  <div class="menu-separator" role="separator"></div>
  <MenuItem
    bind:el={switchRow}
    icon="users"
    chevron
    chevronLeft={switchOpensLeft}
    active={switchOpen}
    onclick={toggleSwitch}
  >{$t('account.switchAccount')}</MenuItem>
  <MenuItem icon="logout" danger onclick={() => choose('signout')}>{$t('account.signOut')}</MenuItem>

  {#if languageOpen}
    <Menu
      bind:open={languageOpen}
      bind:opensLeft={languageOpensLeft}
      label={$t('settings.language')}
      placement="end"
      width="full"
      inset
      match={footer}
      insetParent={drawer}
      anchor={languageRow}
      submenuAnchorRow={0}
      group={MENU_GROUP}
      listClass="chat-drawer-profile-submenu chat-drawer-profile-language"
    >
      <div class="language-menu-search">
        <Icon name="search" size={13}/>
        <input
          bind:this={languageSearchEl}
          bind:value={languageQuery}
          type="search"
          placeholder={$t('settings.searchLanguages')}
          aria-label={$t('settings.searchLanguages')}
          spellcheck="false"
          autocomplete="off"
        />
        {#if languageQuery}
          <button
            type="button"
            class="model-menu-clear"
            aria-label={$t('common.clearSearch')}
            data-tooltip="none"
            onclick={() => { languageQuery = ''; languageSearchEl?.focus(); }}
          ><Icon name="close" size={12}/></button>
        {/if}
      </div>
      <div class="language-menu-list" use:scrollFade={languageQuery}>
        {#each languageMatches as option (option.value)}
          <MenuItem
            role="menuitemradio"
            checked={language === option.value}
            onclick={() => void setLanguage(option.value)}
          >{option.label}</MenuItem>
        {:else}
          <p class="language-menu-empty">{$t('settings.noLanguageMatches')}</p>
        {/each}
      </div>
    </Menu>
  {/if}

  {#if switchOpen}
    <Menu
      bind:open={switchOpen}
      bind:opensLeft={switchOpensLeft}
      label={$t('account.switchAccount')}
      placement="end"
      width="full"
      inset
      match={footer}
      insetParent={drawer}
      anchor={switchRow}
      submenuAnchorRow={0}
      group={MENU_GROUP}
      listClass="chat-drawer-profile-submenu"
    >
      {#each others as account (account.userId)}
        <MenuItem icon="user" onclick={() => void switchTo(account.userId)}>
          {account.email || account.name}
        </MenuItem>
      {/each}
      {#if others.length}
        <div class="menu-separator" role="separator"></div>
      {/if}
      <MenuItem icon="plus" onclick={() => choose('add')}>{$t('account.addAccount')}</MenuItem>
    </Menu>
  {/if}
</Menu>
