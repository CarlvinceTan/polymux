<script lang="ts">
  import AssistantDevicePicker from '../team/AssistantDevicePicker.svelte';
  export let devices: import('@polymux/protocol').TeamHostDto[] = [];
  export let deviceId = '';
  export let deviceLocked = false;
  export let onDeviceChange: (id: string) => void = () => {};
  import {onMount, tick} from 'svelte';
  import {createDictation} from '../../shared/components/dictation';
  import {polymuxApi} from '../../api/polymux';
  import type {ModelDto, ReasoningEffort} from '@polymux/protocol';
  import Icon from '../../shared/components/Icon.svelte';
  import InlineChip, {type InlineChipItem, type SubmittedChip} from './InlineChip.svelte';
  import ProviderLogo from '../../shared/components/ProviderLogo.svelte';
  import {loadAgentDraft, saveAgentDraft} from '../../shared/state/composerDrafts';
  import {t, translate} from '../../../i18n';
  import {MENU_EDGE_MARGIN, clampToMenuEdge} from '../../shared/layout/menuPlacement';

  export let active = false;
  export let speechModeEnabled = true;
  /** Seconds of silence that end dictation, or null to listen until pressed again. */
  export let dictationAutoStopSeconds: number | null = 6;
  /** Empty means the default prompt, which follows the language. */
  export let placeholder = '';
  export let variant: 'default' | 'welcome' = 'default';
  /** `immediate` sends past the queue and steers a run that is already going. */
  export let onSend: (text: string, files: File[], asGoal: boolean, immediate: boolean) => void;
  export let onStop: () => void = () => {};
  export let onVoice: () => void = () => {};
  export let reasoning: ReasoningEffort = 'medium';
  export let onReasoningChange: (value: ReasoningEffort) => void = () => {};
  /** Text to drop into the draft, keyed so the same request applies once. */
  export let insertion: {id: string; text: string} | null = null;
  export let onInsertionApplied: () => void = () => {};
  /** Lets the shell tint the actual agent pane instead of styling this input. */
  export let onFileDragActiveChange: (active: boolean) => void = () => {};
  /** Conversation-local persistence key. The parent remounts the composer when
   * this changes, so each chat gets its own recovered text. */
  export let draftKey = 'new';

  const api = polymuxApi();

  type Attachment = InlineChipItem & {file: File};

  let draft = loadAgentDraft(draftKey);
  let attachments: Attachment[] = [];
  let editor: InlineChip;
  let fileInput: HTMLInputElement;
  let expanded = false;
  let goalEnabled = false;
  let fileDragActive = false;

  $: reasoningEffortOptions = [
    {value: 'off' as ReasoningEffort, label: $t('reasoning.off')},
    {value: 'low' as ReasoningEffort, label: $t('reasoning.low')},
    {value: 'medium' as ReasoningEffort, label: $t('reasoning.medium')},
    {value: 'high' as ReasoningEffort, label: $t('reasoning.high')},
  ];
  /** Models that always think but take no effort level: neither pi-ai nor the
      models.dev catalogue records adjustable levels, so the exceptions live
      here and the menu offers them a single Default. */
  const fixedEffortModels = [/-reasoner\b/, /^grok-4(?!.*mini)/, /-pro\b/];
  let modelMenuOpen = false;
  let modelWrap: HTMLDivElement;
  let modelButton: HTMLButtonElement;
  let modelMenuEl: HTMLElement;
  /** Only models whose provider is configured — the ones a run can actually
      use — reach the menu. */
  let availableModels: ModelDto[] = [];
  let providerLogos: Record<string, string | undefined> = {};
  let modelsLoaded = false;
  let modelSearch = '';
  let searchField: HTMLInputElement;
  /** The row whose reasoning submenu is showing, keyed provider/id. */
  let openModelKey = '';
  let modelSubmenu: HTMLElement;
  let modelRow: HTMLElement;

  const modelKey = (model: ModelDto): string => `${model.provider}/${model.id}`;
  const adjustable = (model: ModelDto): boolean =>
    model.reasoning && !fixedEffortModels.some((pattern) => pattern.test(model.id));

  let dictationListening = false;
  let dictationError = '';
  const dictation = createDictation({
    getText: () => draft,
    caret: () => editor?.caret() ?? draft.length,
    setText: (text, caret) => { draft = text; editor?.setText(text, caret); },
    autoStopSeconds: () => dictationAutoStopSeconds,
    onState: (listening, error) => { dictationListening = listening; dictationError = error; },
  });
  const toggleDictation = () => dictation.toggle();
  const cancelDictation = () => dictation.cancel();

  $: hasContent = draft.length > 0 || attachments.length > 0;
  /** Content is always sendable; with an empty composer the button offers to
      stop a run, and otherwise it opens speech mode. */
  $: primary = (hasContent ? 'send' : active ? 'stop' : speechModeEnabled ? 'mic' : 'send') as 'send' | 'stop' | 'mic';
  $: chips = attachments.map(({file: _file, ...chip}) => chip);
  $: openModel = modelMenuItems.find((model) => modelKey(model) === openModelKey) ?? null;
  /** The selected model leads the list so the current choice is visible without
      scrolling; the rest keep their original order. */
  $: modelMenuItems = availableModels
    .filter((model) => {
      const query = modelSearch.trim().toLowerCase();
      return !query || `${model.name} ${model.provider}`.toLowerCase().includes(query);
    })
    .sort((a, b) => Number(b.selected) - Number(a.selected));
  $: applyInsertion(insertion);

  let appliedInsertionId = '';

  function applyInsertion(next: {id: string; text: string} | null): void {
    if (!next || next.id === appliedInsertionId) return;
    appliedInsertionId = next.id;
    draft = draft ? `${draft} ${next.text}` : next.text;
    saveAgentDraft(draftKey, draft);
    editor?.setText(draft);
    onInsertionApplied();
  }

  function changeDraft(text: string): void {
    draft = text;
    saveAgentDraft(draftKey, text);
  }

  async function primaryAction(): Promise<void> {
    if (primary === 'stop') onStop();
    else if (primary === 'mic') {
      const permission = await api.permissions.request('microphone');
      if (permission === 'granted') onVoice();
      else dictationError = translate('dictation.noPermission');
    }
    else editor?.submit();
  }

  function submit(text: string, ordered: SubmittedChip[], immediate = false): void {
    const trimmed = text.trim();
    const ids = new Set(ordered.map((chip) => chip.id));
    const files = attachments.filter((attachment) => ids.has(attachment.id)).map((attachment) => attachment.file);
    if (!trimmed && !files.length) return;
    cancelDictation();
    onSend(trimmed, files.length ? files : attachments.map((attachment) => attachment.file), goalEnabled, immediate);
    goalEnabled = false;
    draft = '';
    saveAgentDraft(draftKey, '');
    attachments = [];
    editor?.setText('');
  }

  function removeAttachment(id: string): void {
    attachments = attachments.filter((attachment) => attachment.id !== id);
  }

  /** Levels when the model takes them, otherwise the one state it is fixed at:
      Default for an always-on reasoner, None for a model that never thinks. */
  function effortsFor(model: ModelDto): Array<{value: ReasoningEffort; label: string}> {
    return adjustable(model)
      ? reasoningEffortOptions
      : [{value: reasoning, label: model.reasoning ? $t('reasoning.default') : $t('reasoning.none')}];
  }

  /** The list hangs off the MODEL word, at body level so a drawer or scroller
      cannot clip it. Welcome drops it below; a conversation lifts it above. */
  function floatMenu(node: HTMLElement, side: 'default' | 'welcome') {
    document.body.appendChild(node);
    const gap = 6;
    const margin = 8;
    let openDown = side === 'welcome';
    const place = () => {
      const button = modelButton;
      if (!button) return;
      const word = (button.querySelector('span:last-of-type') ?? button).getBoundingClientRect();
      const trigger = button.getBoundingClientRect();
      const box = node.getBoundingClientRect();
      const left = Math.max(margin, Math.min(
        word.left + word.width / 2 - box.width / 2,
        window.innerWidth - margin - box.width,
      ));
      let top = openDown ? trigger.bottom + gap : trigger.top - gap - box.height;
      top = Math.max(margin, Math.min(top, window.innerHeight - margin - box.height));
      node.style.left = `${left}px`;
      node.style.top = `${top}px`;
      node.style.visibility = 'visible';
    };
    node.style.visibility = 'hidden';
    place();
    const observer = new ResizeObserver(place);
    observer.observe(node);
    window.addEventListener('resize', place);
    window.addEventListener('scroll', place, true);
    return {
      update(next: 'default' | 'welcome') {
        openDown = next === 'welcome';
        place();
      },
      destroy() {
        observer.disconnect();
        window.removeEventListener('resize', place);
        window.removeEventListener('scroll', place, true);
        node.remove();
      },
    };
  }

  /** Escape the conversation scroller and measure the actual rendered menu. */
  function floatSubmenu(node: HTMLElement, _key: string) {
    document.body.appendChild(node);
    const place = () => {
      const row = modelRow?.getBoundingClientRect();
      const menu = modelRow?.closest('.model-menu')?.getBoundingClientRect();
      if (!row || !menu) return;
      const box = node.getBoundingClientRect();
      const right = menu.right + 4;
      const left = right + box.width <= window.innerWidth - MENU_EDGE_MARGIN
        ? right : menu.left - 4 - box.width;
      node.style.left = `${clampToMenuEdge(left, box.width, window.innerWidth)}px`;
      node.style.top = `${clampToMenuEdge(row.top - 4, box.height, window.innerHeight)}px`;
    };
    place();
    const observer = new ResizeObserver(place);
    observer.observe(node);
    window.addEventListener('resize', place);
    window.addEventListener('scroll', place, true);
    return {
      update: place,
      destroy() {
        observer.disconnect();
        window.removeEventListener('resize', place);
        window.removeEventListener('scroll', place, true);
        node.remove();
      },
    };
  }

  function openRow(model: ModelDto, row: HTMLElement): void {
    modelRow = row;
    openModelKey = modelKey(model);
  }

  /** One choice settles both halves — which model runs and how hard it thinks —
      so picking a level closes the menu and its submenu together. */
  async function chooseModel(model: ModelDto, value: ReasoningEffort): Promise<void> {
    closeModelMenu();
    if (!model.selected) {
      try {
        await api.models.select(model.provider, model.id);
        availableModels = availableModels.map((item) => ({...item, selected: item === model}));
      } catch {
        // Leaving the selection where it was is the whole recovery: the model
        // list reloads the next time the menu opens.
        modelsLoaded = false;
      }
    }
    if (value !== reasoning) onReasoningChange(value);
  }

  function closeModelMenu(): void {
    modelMenuOpen = false;
    openModelKey = '';
    modelSearch = '';
  }

  async function toggleModelMenu(): Promise<void> {
    if (modelMenuOpen) {
      closeModelMenu();
      return;
    }
    modelMenuOpen = true;
    openModelKey = '';
    modelSearch = '';
    if (!modelsLoaded) {
      try {
        const [models, providers] = await Promise.all([api.models.list(), api.providers.list()]);
        const configured = new Set(providers.filter((provider) => provider.configured).map((provider) => provider.id));
        providerLogos = Object.fromEntries(providers.map((provider) => [provider.id, provider.logoDataUrl]));
        availableModels = models.filter((model) => model.custom || configured.has(model.provider));
        modelsLoaded = true;
      } catch {
        availableModels = [];
      }
    }
    // The search takes the caret so typing filters straight away.
    await tick();
    searchField?.focus();
  }

  function dismissModelMenu(event: MouseEvent): void {
    const target = event.target as Node;
    // A control the click itself removed — the clear button going away with the
    // text it cleared — is no longer inside anything, so containment would read
    // it as an outside click and close the menu under the user.
    if (!target.isConnected) return;
    if (modelMenuOpen && !modelWrap?.contains(target) && !modelMenuEl?.contains(target) && !modelSubmenu?.contains(target)) closeModelMenu();
  }

  function modelMenuKeydown(event: KeyboardEvent): void {
    if (modelMenuOpen && event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      if (openModelKey) openModelKey = '';
      else closeModelMenu();
    }
  }

  function chooseFiles(): void {
    fileInput?.click();
  }

  function addFiles(files: Iterable<File>): void {
    const added = Array.from(files).map((file) => {
      const id = crypto.randomUUID();
      return {id, localId: id, name: file.name, status: 'done' as const, progress: 100, file};
    });
    if (added.length) attachments = [...attachments, ...added];
  }

  function selected(event: Event): void {
    const input = event.currentTarget as HTMLInputElement;
    addFiles(input.files ?? []);
    input.value = '';
  }

  function isFileDrag(event: DragEvent): boolean {
    return Array.from(event.dataTransfer?.types ?? []).includes('Files') || Boolean(event.dataTransfer?.files.length);
  }

  /** A workspace pane can claim its own file drop. The prompt still accepts a
   * drop anywhere else in the window, but it must not intercept one whose
   * nearest destination is another composer. */
  function belongsToAnotherDropScope(event: DragEvent): boolean {
    const target = event.target;
    if (!(target instanceof Element)) return false;
    const scope = target.closest<HTMLElement>('[data-file-drop-scope]');
    return Boolean(scope && scope.dataset.fileDropScope !== 'agent');
  }

  /** The agent accepts files across its whole pane, not across the whole
   * window. Re-checking the actual pane bounds on every drag event also gives
   * the neutral space between the agent and workspace a clean no-target state. */
  function isInsideAgentDropPane(event: DragEvent): boolean {
    const pane = document.querySelector<HTMLElement>('.agent-file-drop-pane-overlay');
    if (!pane) return false;
    const box = pane.getBoundingClientRect();
    return event.clientX >= box.left && event.clientX < box.right &&
      event.clientY >= box.top && event.clientY < box.bottom;
  }

  function isAgentDropTarget(event: DragEvent): boolean {
    return !belongsToAnotherDropScope(event) && isInsideAgentDropPane(event);
  }

  function setFileDragActive(active: boolean): void {
    if (fileDragActive === active) return;
    fileDragActive = active;
    onFileDragActiveChange(active);
  }

  function windowDragEnter(event: DragEvent): void {
    if (!isFileDrag(event)) return;
    event.preventDefault();
    const active = isAgentDropTarget(event);
    setFileDragActive(active);
    if (!active && event.dataTransfer && !belongsToAnotherDropScope(event)) event.dataTransfer.dropEffect = 'none';
  }

  function windowDragOver(event: DragEvent): void {
    if (!isFileDrag(event)) return;
    event.preventDefault();
    const active = isAgentDropTarget(event);
    setFileDragActive(active);
    if (event.dataTransfer && !belongsToAnotherDropScope(event)) event.dataTransfer.dropEffect = active ? 'copy' : 'none';
  }

  function windowDragLeave(event: DragEvent): void {
    if (!event.relatedTarget || !isInsideAgentDropPane(event)) setFileDragActive(false);
  }

  function windowDrop(event: DragEvent): void {
    if (!isFileDrag(event)) return;
    event.preventDefault();
    setFileDragActive(false);
    if (!isAgentDropTarget(event)) return;
    addFiles(event.dataTransfer?.files ?? []);
  }


  onMount(() => {
    // Fetches the speech-to-text model if setup never got to it — on a machine
    // that already has it this costs nothing, and it means the microphone is
    // ready long before anyone presses it. Failures are the download's to
    // report when dictation is actually used.
    if (speechModeEnabled) void api.dictation.prepare().catch(() => {});
    // Capture first so a destination pane can stop bubbling without leaving
    // the previously hovered pane highlighted.
    window.addEventListener('dragenter', windowDragEnter, true);
    window.addEventListener('dragover', windowDragOver, true);
    window.addEventListener('dragleave', windowDragLeave, true);
    window.addEventListener('drop', windowDrop, true);
    window.addEventListener('click', dismissModelMenu);
    window.addEventListener('keydown', modelMenuKeydown);
    return () => {
      cancelDictation();
      setFileDragActive(false);
      window.removeEventListener('dragenter', windowDragEnter, true);
      window.removeEventListener('dragover', windowDragOver, true);
      window.removeEventListener('dragleave', windowDragLeave, true);
      window.removeEventListener('drop', windowDrop, true);
      window.removeEventListener('click', dismissModelMenu);
      window.removeEventListener('keydown', modelMenuKeydown);
    };
  });
</script>

<div
  class:welcome={variant === 'welcome'}
  class:file-drag-active={fileDragActive}
  class="polymux-prompt"
  data-file-drop-scope="agent"
>
  <input bind:this={fileInput} class="visually-hidden" name="prompt-attachments" type="file" multiple tabindex="-1" aria-hidden="true" onchange={selected}/>

  <div class:expanded class:raised={hasContent} class="polymux-prompt-shell">
    <div class="polymux-editor-slot">
      <InlineChip
        bind:this={editor}
        value={draft}
        {chips}
        placeholder={placeholder || $t('composer.placeholder')}
        onChange={changeDraft}
        onSubmit={submit}
        onRemove={removeAttachment}
        onExpanded={(value) => expanded = value}
      />
    </div>
    <button
      type="button"
      data-testid="prompt-primary-button"
      class="polymux-primary"
      aria-label={primary === 'send' ? $t('composer.sendMessage') : primary === 'stop' ? $t('composer.stopAgent') : $t('composer.startSpeechMode')}
      data-tooltip="none"
      onclick={primaryAction}
    ><Icon name={primary === 'mic' ? 'waveform' : primary} size={primary === 'stop' ? 22 : 18}/></button>
  </div>

  <div class="polymux-prompt-toolbar">
    <button type="button" onclick={chooseFiles}><Icon name="attach" size={14}/><span>{$t('composer.attach')}</span></button>
    <button
      class="dictation-toggle"
      class:active={dictationListening}
      type="button"
      aria-pressed={dictationListening}
      onclick={toggleDictation}
    >
      <span class="dictation-mark">
        {#if dictationListening}<span class="dictation-ping" aria-hidden="true"></span>{/if}
        <Icon name="mic" size={14}/>
      </span>
      <span>{dictationListening ? $t('composer.listening') : $t('composer.voice')}</span>
    </button>
    <button
      class="goal-toggle"
      class:active={goalEnabled}
      type="button"
      aria-label={goalEnabled ? $t('composer.goalOff') : $t('composer.goalOn')}
      aria-pressed={goalEnabled}
      onclick={() => goalEnabled = !goalEnabled}
    ><Icon name="goal" size={14}/><span>{$t('composer.goal')}</span></button>
    <AssistantDevicePicker {devices} {deviceId} locked={deviceLocked} onChange={onDeviceChange}/>
    <div bind:this={modelWrap} class="prompt-option-wrap">
      <button bind:this={modelButton} type="button" aria-haspopup="menu" aria-expanded={modelMenuOpen} disabled={devices.some(device => device.hostId === deviceId && device.mode === 'remote')} data-tooltip-label={devices.some(device => device.hostId === deviceId && device.mode === 'remote') ? 'Model is configured on the selected device' : undefined} onclick={() => void toggleModelMenu()}>
        <Icon name="brain" size={14}/>
        <span>{$t('composer.model')}</span>
      </button>
      {#if modelMenuOpen && modelsLoaded}
        <div bind:this={modelMenuEl} use:floatMenu={variant} class="polymux-dropdown-menu model-menu" role="menu" aria-label={$t('composer.modelOptions')}>
          <div class="model-menu-search">
            <Icon name="search" size={13}/>
            <input
              bind:this={searchField}
              bind:value={modelSearch}
              type="text"
              placeholder={$t('composer.searchModels')}
              aria-label={$t('composer.searchModels')}
              spellcheck="false"
              autocomplete="off"
            />
            {#if modelSearch}
              <button
                type="button"
                class="model-menu-clear"
                aria-label={$t('common.clearSearch')}
                data-tooltip="none"
                onclick={() => { modelSearch = ''; searchField?.focus(); }}
              ><Icon name="close" size={12}/></button>
            {/if}
          </div>
          <div class="model-menu-list" onscroll={() => openModelKey = ''}>
            {#each modelMenuItems as model (modelKey(model))}
              <div class="model-menu-row">
                <button
                  type="button"
                  class="polymux-dropdown-item"
                  class:active={openModelKey === modelKey(model)}
                  role="menuitem"
                  aria-haspopup="menu"
                  aria-expanded={openModelKey === modelKey(model)}
                  onmouseenter={(event) => openRow(model, event.currentTarget)}
                  onfocus={(event) => openRow(model, event.currentTarget)}
                  onclick={(event) => openRow(model, event.currentTarget)}
                >
                  <span class="model-menu-mark"><ProviderLogo provider={model.provider} logoDataUrl={providerLogos[model.provider]} size={14}/></span>
                  <span class="model-menu-name">{model.name}</span>
                  <span class="reasoning-menu-check" aria-hidden="true">
                    {#if model.selected}<Icon name="check" size={13}/>{/if}
                  </span>
                  <span class="model-menu-caret" aria-hidden="true"><Icon name="chevron" size={12}/></span>
                </button>
              </div>
            {:else}
              <p class="model-menu-empty">{$t('composer.noModels')}</p>
            {/each}
          </div>
          <!-- Outside the scroller: a submenu inside it would be clipped by the
               overflow that makes the list scrollable. -->
          {#if openModel}
            <div bind:this={modelSubmenu} use:floatSubmenu={openModelKey} class="polymux-dropdown-menu model-submenu" role="menu" aria-label={$t('composer.reasoningFor', {model: openModel.name})}>
              <p class="model-submenu-title">{$t('composer.reasoning')}</p>
              {#each effortsFor(openModel) as option (option.value)}
                <button
                  type="button"
                  class="polymux-dropdown-item"
                  role="menuitemradio"
                  aria-checked={openModel.selected && option.value === reasoning}
                  aria-disabled={!adjustable(openModel)}
                  onclick={() => void chooseModel(openModel, option.value)}
                >
                  <span>{option.label}</span>
                  <span class="reasoning-menu-check" aria-hidden="true">
                    {#if openModel.selected && option.value === reasoning}<Icon name="check" size={13}/>{/if}
                  </span>
                </button>
              {/each}
            </div>
          {/if}
        </div>
      {/if}
    </div>
  </div>
  {#if dictationError}<p class="dictation-error" role="alert">{dictationError}</p>{/if}
</div>
