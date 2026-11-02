<script lang="ts">
  import {fade} from 'svelte/transition';
  import type {AccountStatusDto} from '@polymux/protocol';
  import Icon from '../../shared/components/Icon.svelte';
  import AccountBrandMark from './AccountBrandMark.svelte';
  import AccountMark from './AccountMark.svelte';
  import {polymuxApi} from '../../api/polymux';
  import {t} from '../../../i18n';

  const api = polymuxApi();
  const TERMS_URL = 'https://polymux.com/privacy-policy/';
  const PRIVACY_URL = 'https://polymux.com/privacy-policy/';
  const MIN_PASSWORD = 6;

  let {
    signedIn = false,
    onClose = () => {},
    onChanged = () => {},
  }: {
    signedIn?: boolean;
    onClose?: () => void;
    /** Called after the account state may have changed, so the row can refresh. */
    onChanged?: (status: AccountStatusDto) => void;
  } = $props();

  type Mode = 'sign-in' | 'sign-up' | 'forgot';

  let mode = $state<Mode>('sign-in');
  let email = $state('');
  let password = $state('');
  let passwordConfirm = $state('');
  let busy = $state(false);
  let pendingProvider = $state<'google' | 'apple' | null>(null);
  let error = $state('');
  let checkEmailSent = $state(false);
  let resetLinkSent = $state(false);
  let passwordUpdated = $state(false);

  const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  const resetting = $derived(signedIn && mode === 'forgot' && resetLinkSent);
  const showForgotBack = $derived(mode === 'forgot' && !resetting && !passwordUpdated);
  const title = $derived(
    passwordUpdated ? $t('account.resetPassword')
      : resetting ? $t('account.resetPassword')
      : checkEmailSent ? $t('account.checkYourEmail')
      : resetLinkSent && mode === 'forgot' ? $t('account.forgotPasswordTitle')
      : mode === 'sign-up' ? $t('account.createYourAccount')
      : mode === 'forgot' ? $t('account.forgotPasswordTitle')
      : $t('account.welcomeBack'),
  );

  function clearMessages(): void {
    error = '';
  }

  function goSignIn(): void {
    mode = 'sign-in';
    password = '';
    passwordConfirm = '';
    checkEmailSent = false;
    resetLinkSent = false;
    passwordUpdated = false;
    pendingProvider = null;
    clearMessages();
  }

  function validatePassword(): string {
    if (password.length < MIN_PASSWORD) return $t('account.passwordMinLength');
    if (password !== passwordConfirm) return $t('account.passwordsDoNotMatch');
    return '';
  }

  async function submitSignIn(): Promise<void> {
    const address = email.trim();
    if (!address || !password) return;
    busy = true;
    clearMessages();
    try {
      const result = await api.account.signInWithPassword(address, password);
      if (result.signedIn) {
        onChanged(result);
        onClose();
        return;
      }
      error = result.error ?? $t('account.errorGeneric');
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      busy = false;
    }
  }

  async function submitSignUp(): Promise<void> {
    const address = email.trim();
    if (!address) return;
    const invalid = validatePassword();
    if (invalid) {
      error = invalid;
      return;
    }
    busy = true;
    clearMessages();
    try {
      const result = await api.account.signUp(address, password);
      if (result.signedIn) {
        onChanged(result);
        onClose();
        return;
      }
      if (result.error) {
        error = result.error;
        return;
      }
      checkEmailSent = true;
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      busy = false;
    }
  }

  async function submitForgot(): Promise<void> {
    const address = email.trim();
    if (!address) return;
    busy = true;
    clearMessages();
    try {
      const result = await api.account.requestPasswordReset(address);
      if (!result.ok) {
        error = result.error ?? $t('account.errorGeneric');
        return;
      }
      resetLinkSent = true;
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      busy = false;
    }
  }

  async function submitNewPassword(): Promise<void> {
    const invalid = validatePassword();
    if (invalid) {
      error = invalid;
      return;
    }
    busy = true;
    clearMessages();
    try {
      const result = await api.account.updatePassword(password);
      if (result.error) {
        error = result.error;
        return;
      }
      onChanged(result);
      passwordUpdated = true;
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      busy = false;
    }
  }

  async function submit(): Promise<void> {
    if (busy) return;
    if (resetting) {
      await submitNewPassword();
      return;
    }
    if (mode === 'forgot') {
      await submitForgot();
      return;
    }
    if (mode === 'sign-up') {
      await submitSignUp();
      return;
    }
    await submitSignIn();
  }

  async function oauth(provider: 'google' | 'apple'): Promise<void> {
    if (busy || pendingProvider) return;
    pendingProvider = provider;
    busy = true;
    clearMessages();
    try {
      const result = await api.account.signInWithOAuth(provider);
      if (result.signedIn) {
        onChanged(result);
        onClose();
        return;
      }
      error = result.error ?? $t('account.errorGeneric');
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      busy = false;
      pendingProvider = null;
    }
  }

  function openPolicy(url: string): void {
    void api.browser.openExternal(url);
  }

  function keydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      onClose();
    }
  }
</script>

<svelte:window onkeydown={keydown}/>

<div
  class="account-signin-backdrop"
  role="presentation"
  onclick={(event) => { if (event.target === event.currentTarget) onClose(); }}
>
  <div
    class="account-signin"
    role="dialog"
    aria-modal="true"
    aria-labelledby="account-signin-title"
    transition:fade={{duration: reducedMotion ? 0 : 120}}
  >
    {#if showForgotBack}
      <button type="button" class="account-signin-back" aria-label={$t('common.back')} data-tooltip="none" onclick={goSignIn}>
        <Icon name="back" size={18}/>
      </button>
    {/if}
    <button type="button" class="account-signin-close" aria-label={$t('account.close')} data-tooltip="none" onclick={onClose}>
      <Icon name="close" size={17}/>
    </button>

    <div class="account-signin-brand"><AccountMark size={52}/></div>
    <h2 id="account-signin-title">{title}</h2>

    {#if checkEmailSent}
      <p class="account-signin-lead">{$t('account.emailConfirmationSent', {email: email.trim()})}</p>
      <button type="button" class="account-signin-primary" onclick={goSignIn}>{$t('account.signIn')}</button>
    {:else if passwordUpdated}
      <div class="account-signin-status" role="status">
        <p>{$t('account.passwordUpdated')}</p>
      </div>
      <button type="button" class="account-signin-primary" onclick={onClose}>{$t('account.signIn')}</button>
    {:else if resetting}
      <p class="account-signin-lead">{$t('account.resetPasswordDesc')}</p>
      <form novalidate onsubmit={(event) => { event.preventDefault(); void submit(); }}>
        <label>
          <span>{$t('account.newPassword')}</span>
          <input type="password" autocomplete="new-password" placeholder="••••••••" bind:value={password} disabled={busy}/>
        </label>
        <label>
          <span>{$t('account.confirmNewPassword')}</span>
          <input type="password" autocomplete="new-password" placeholder="••••••••" bind:value={passwordConfirm} disabled={busy}/>
        </label>
        {#if error}<p class="account-signin-error" role="alert">{error}</p>{/if}
        <button type="submit" class="account-signin-primary" disabled={busy || !password || !passwordConfirm}>
          {busy ? $t('account.updatingPassword') : $t('account.updatePassword')}
        </button>
      </form>
      <p class="account-signin-switch">
        <button type="button" onclick={goSignIn}>{$t('account.backToSignIn')}</button>
      </p>
    {:else if mode === 'forgot'}
      <p class="account-signin-lead">{$t('account.forgotPasswordDesc')}</p>
      {#if resetLinkSent}
        <div class="account-signin-status" role="status">
          <strong>{$t('account.sendResetLink')}</strong>
          <p>{$t('account.resetLinkSent')}</p>
        </div>
      {:else}
        <form novalidate onsubmit={(event) => { event.preventDefault(); void submit(); }}>
          <label>
            <span>{$t('account.email')}</span>
            <input type="email" autocomplete="email" placeholder={$t('account.emailPlaceholder')} bind:value={email} disabled={busy}/>
          </label>
          {#if error}<p class="account-signin-error" role="alert">{error}</p>{/if}
          <button type="submit" class="account-signin-primary" disabled={busy || !email.trim()}>
            {busy ? $t('account.sendingResetLink') : $t('account.sendResetLink')}
          </button>
        </form>
      {/if}
      <p class="account-signin-switch">
        <button type="button" onclick={goSignIn}>{$t('account.backToSignIn')}</button>
      </p>
    {:else}
      <p class="account-signin-lead">{mode === 'sign-up' ? $t('account.signUpToGetStarted') : $t('account.signInToAccount')}</p>

      <div class="account-signin-providers">
        <button type="button" disabled={busy} onclick={() => void oauth('google')}>
          <AccountBrandMark brand="google"/>
          <span>{pendingProvider === 'google' ? $t('account.connecting') : $t('account.google')}</span>
        </button>
        <button type="button" disabled={busy} onclick={() => void oauth('apple')}>
          <AccountBrandMark brand="apple"/>
          <span>{pendingProvider === 'apple' ? $t('account.connecting') : $t('account.apple')}</span>
        </button>
      </div>
      <div class="account-signin-divider" role="separator"><span>{$t('account.or')}</span></div>

      <form novalidate onsubmit={(event) => { event.preventDefault(); void submit(); }}>
        <label>
          <span>{$t('account.email')}</span>
          <input type="email" autocomplete="email" placeholder={$t('account.emailPlaceholder')} bind:value={email} disabled={busy}/>
        </label>
        {#if mode === 'sign-in'}
          <div class="account-signin-field">
            <div class="account-signin-password-label">
              <label for="account-signin-password">{$t('account.password')}</label>
              <button type="button" onclick={() => { mode = 'forgot'; password = ''; passwordConfirm = ''; checkEmailSent = false; resetLinkSent = false; clearMessages(); }}>
                {$t('account.forgotPassword')}
              </button>
            </div>
            <input id="account-signin-password" type="password" autocomplete="current-password" placeholder="••••••••" bind:value={password} disabled={busy}/>
          </div>
        {:else}
          <label>
            <span>{$t('account.createPassword')}</span>
            <input type="password" autocomplete="new-password" placeholder="••••••••" bind:value={password} disabled={busy}/>
          </label>
          <label>
            <span>{$t('account.confirmYourPassword')}</span>
            <input type="password" autocomplete="new-password" placeholder="••••••••" bind:value={passwordConfirm} disabled={busy}/>
          </label>
        {/if}
        {#if error}<p class="account-signin-error" role="alert">{error}</p>{/if}
        <button type="submit" class="account-signin-primary" disabled={busy || !email.trim() || !password || (mode === 'sign-up' && !passwordConfirm)}>
          {#if busy}
            {mode === 'sign-up' ? $t('account.creatingAccount') : $t('account.signingIn')}
          {:else}
            {mode === 'sign-up' ? $t('account.createAccount') : $t('account.signIn')}
          {/if}
        </button>
      </form>

      <p class="account-signin-switch">
        {mode === 'sign-in' ? $t('account.dontHaveAccount') : $t('account.alreadyHaveAccount')}
        <button
          type="button"
          onclick={() => {
            mode = mode === 'sign-in' ? 'sign-up' : 'sign-in';
            password = '';
            passwordConfirm = '';
            checkEmailSent = false;
            clearMessages();
          }}
        >
          {mode === 'sign-in' ? $t('account.signUp') : $t('account.signIn')}
        </button>
      </p>
    {/if}

    {#if !checkEmailSent && !passwordUpdated}
      <p class="account-signin-terms">
        {$t('account.agreeToTerms')}
        <a href={TERMS_URL} onclick={(event) => { event.preventDefault(); openPolicy(TERMS_URL); }}>{$t('account.termsOfService')}</a>
        {$t('account.and')}
        <a href={PRIVACY_URL} onclick={(event) => { event.preventDefault(); openPolicy(PRIVACY_URL); }}>{$t('account.privacyPolicy')}</a>{$t('account.andReceiveEmails')}
      </p>
    {/if}
  </div>
</div>

<style>
  .account-signin-backdrop {
    position: fixed;
    z-index: 1100;
    inset: 0;
    display: grid;
    justify-items: center;
    align-items: center;
    padding: 24px;
    background: rgba(20, 20, 20, .24);
    backdrop-filter: blur(5px);
  }
  .account-signin {
    position: relative;
    box-sizing: border-box;
    width: min(420px, 100%);
    max-height: calc(100vh - 48px);
    overflow: auto;
    scrollbar-width: none;
    border-radius: 16px;
    background: var(--app-surface);
    box-shadow: 0 1px 3px rgba(0, 0, 0, .06);
    outline: 1px solid var(--neutral-200);
    padding: 20px 24px 18px;
  }
  .account-signin::-webkit-scrollbar { display: none; }
  .account-signin-back,
  .account-signin-close {
    position: absolute;
    top: 12px;
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: var(--neutral-500);
    cursor: pointer;
  }
  .account-signin-back { left: 12px; }
  .account-signin-close { right: 12px; }
  .account-signin-back:hover,
  .account-signin-close:hover { color: var(--neutral-950); }
  .account-signin-brand { display: flex; justify-content: center; margin: 4px 0 10px; }
  h2 {
    margin: 0;
    text-align: center;
    font-size: 20px;
    font-weight: 600;
    letter-spacing: -.02em;
    color: var(--neutral-950);
  }
  .account-signin-lead {
    margin: 6px 0 16px;
    text-align: center;
    font-size: 13px;
    line-height: 1.5;
    color: var(--neutral-500);
  }
  .account-signin-providers { display: flex; flex-direction: column; gap: 8px; }
  .account-signin-providers button {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    min-height: 40px;
    border: 1px solid var(--neutral-300);
    border-radius: 8px;
    background: var(--app-surface);
    color: var(--neutral-800);
    font-size: 13.5px;
    font-weight: 500;
    cursor: pointer;
  }
  .account-signin-providers button:hover:not(:disabled) { background: var(--neutral-50); }
  .account-signin-providers button:disabled { opacity: .5; cursor: default; }
  .account-signin-divider {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: 16px 0;
    color: var(--neutral-400);
    font-size: 11px;
    font-weight: 650;
    letter-spacing: .08em;
    text-transform: uppercase;
  }
  .account-signin-divider::before, .account-signin-divider::after {
    content: '';
    flex: 1;
    height: 1px;
    background: var(--neutral-200);
  }
  form { display: flex; flex-direction: column; gap: 12px; margin: 0; }
  label, .account-signin-field { display: flex; flex-direction: column; gap: 6px; font-size: 13px; font-weight: 500; color: var(--neutral-800); }
  .account-signin-password-label { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; }
  .account-signin-password-label label { display: inline; margin: 0; }
  .account-signin-password-label button,
  .account-signin-switch button,
  .account-signin-terms a {
    display: inline;
    padding: 0;
    margin: 0;
    border: 0;
    appearance: none;
    background: none;
    color: var(--neutral-600);
    font: inherit;
    font-weight: 500;
    line-height: inherit;
    text-decoration: underline;
    text-decoration-color: var(--neutral-300);
    text-underline-offset: 2px;
    cursor: pointer;
    vertical-align: baseline;
  }
  .account-signin-password-label button:hover,
  .account-signin-switch button:hover,
  .account-signin-terms a:hover { color: var(--neutral-950); }
  input {
    box-sizing: border-box;
    width: 100%;
    min-height: 40px;
    border: 1px solid var(--neutral-300);
    border-radius: 8px;
    padding: 0 12px;
    background: var(--app-bg);
    color: var(--neutral-950);
    font: inherit;
    font-size: 13.5px;
    font-weight: 400;
  }
  input::placeholder { color: var(--neutral-400); }
  input:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: -1px; }
  .account-signin-primary {
    min-height: 40px;
    border: 0;
    border-radius: 8px;
    background: var(--neutral-950);
    color: var(--on-primary);
    font-size: 13.5px;
    font-weight: 500;
    cursor: pointer;
  }
  .account-signin-primary:hover:not(:disabled) { opacity: .9; }
  .account-signin-primary:disabled { opacity: .5; cursor: default; }
  .account-signin-error {
    margin: 0;
    padding: 10px 12px;
    border-radius: 8px;
    background: color-mix(in srgb, var(--danger, #b42318) 10%, transparent);
    color: var(--danger, #b42318);
    font-size: 13px;
    line-height: 1.45;
    overflow-wrap: anywhere;
  }
  .account-signin-status {
    margin: 0 0 16px;
    padding: 12px 14px;
    border: 1px solid var(--neutral-200);
    border-radius: 8px;
    background: var(--neutral-50);
    color: var(--neutral-700);
    font-size: 13px;
    line-height: 1.45;
  }
  .account-signin-status strong,
  .account-signin-status p { display: block; }
  .account-signin-status strong { margin-bottom: 4px; color: var(--neutral-950); font-weight: 600; }
  .account-signin-status p { margin: 0; }
  .account-signin-switch {
    margin: 16px 0 0;
    text-align: center;
    color: var(--neutral-500);
    font-size: 13px;
  }
  .account-signin-switch button { color: var(--neutral-950); }
  .account-signin-terms {
    margin: 16px auto 0;
    max-width: 22rem;
    text-align: center;
    color: var(--neutral-500);
    font-size: 11px;
    line-height: 1.5;
  }
  .account-signin-terms a { color: var(--neutral-700); font-size: inherit; }
  @media (prefers-reduced-motion: reduce) { .account-signin { transition: none; } }
</style>
