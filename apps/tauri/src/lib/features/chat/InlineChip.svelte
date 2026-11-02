<script module lang="ts">
  export type ChipStatus = 'pending' | 'uploading' | 'done' | 'error';

  export type InlineChipItem = {
    id: string;
    localId: string;
    name: string;
    status?: ChipStatus;
    progress?: number;
  };

  export type SubmittedChip = {
    id: string;
    name: string;
    position: number;
  };
</script>

<script lang="ts">
  import {afterUpdate, mount, onDestroy, onMount, unmount} from 'svelte';
  import FileAttachment from './FileAttachment.svelte';

  const chipComponents = new Map<HTMLElement, Record<string, unknown>>();

  export let value = '';
  export let chips: InlineChipItem[] = [];
  export let placeholder = '';
  export let ariaLabel = 'Message Polymux';
  export let disabled = false;
  export let maxLines = 4;
  export let onChange: (text: string) => void = () => {};
  /** `immediate` is set when the send was made with Cmd/Ctrl+Enter, which skips
      the queue and interrupts a running agent. */
  export let onSubmit: (text: string, chips: SubmittedChip[], immediate: boolean) => void = () => {};
  export let onRemove: (id: string) => void = () => {};
  export let onExpanded: (expanded: boolean) => void = () => {};

  let editor: HTMLDivElement;
  let savedRange: Range | null = null;
  let suppressInput = false;
  let lastEmittedText = value;
  let isEmpty = value.length === 0 && chips.length === 0;
  let expanded = false;
  let mounted = false;
  let accessibilityObserver: MutationObserver | undefined;

  function escapeSelector(text: string): string {
    return globalThis.CSS?.escape ? globalThis.CSS.escape(text) : text.replace(/["\\]/g, '\\$&');
  }

  function walkEditor(onText: (text: string) => void, onChip: (element: HTMLElement) => void): void {
    if (!editor) return;

    function walk(node: Node): void {
      if (node.nodeType === Node.TEXT_NODE) {
        onText(node.textContent ?? '');
        return;
      }
      if (node.nodeType !== Node.ELEMENT_NODE) return;

      const element = node as HTMLElement;
      if (element.hasAttribute('data-chip')) {
        onChip(element);
        return;
      }
      if (element.tagName === 'BR') {
        onText('\n');
        return;
      }

      const block = element.tagName === 'DIV' || element.tagName === 'P';
      for (const child of element.childNodes) walk(child);
      if (block) onText('\n');
    }

    for (const child of editor.childNodes) walk(child);
  }

  function getText(): string {
    let text = '';
    walkEditor((part) => text += part, () => {});
    return text.endsWith('\n') ? text.slice(0, -1) : text;
  }

  function serialize(): {text: string; chips: SubmittedChip[]} {
    let text = '';
    const ordered: SubmittedChip[] = [];

    walkEditor(
      (part) => text += part,
      (element) => {
        const id = element.dataset.id ?? '';
        const name = element.dataset.name ?? '';
        if (id && element.dataset.status === 'done') {
          ordered.push({id, name, position: text.length});
        }
      },
    );

    if (text.endsWith('\n')) text = text.slice(0, -1);
    return {text, chips: ordered};
  }

  function syncEmpty(): void {
    isEmpty = !editor?.querySelector('[data-chip]') && getText().length === 0;
  }

  function updateExpanded(): void {
    if (!editor) return;
    const lineHeight = Number.parseFloat(getComputedStyle(editor).lineHeight) || 20;
    const next = editor.scrollHeight > lineHeight + 2;
    if (next !== expanded) {
      expanded = next;
      onExpanded(next);
    }
  }

  function rememberSelection(): void {
    const selection = window.getSelection();
    if (!selection?.rangeCount) return;
    const range = selection.getRangeAt(0);
    if (editor?.contains(range.startContainer)) savedRange = range.cloneRange();
  }

  function placeCursorAtEnd(): void {
    const range = document.createRange();
    range.selectNodeContents(editor);
    range.collapse(false);
    apply(range);
  }

  /** Offsets index the text `getText` returns, so callers never have to know
      how the content is laid out in the DOM. */
  function placeCursorAt(offset: number): void {
    const node = editor.firstChild;
    if (node?.nodeType !== Node.TEXT_NODE) {
      placeCursorAtEnd();
      return;
    }
    const range = document.createRange();
    range.setStart(node, Math.max(0, Math.min(offset, node.textContent?.length ?? 0)));
    range.collapse(true);
    apply(range);
  }

  function apply(range: Range): void {
    const selection = window.getSelection();
    selection?.removeAllRanges();
    selection?.addRange(range);
    savedRange = range.cloneRange();
  }

  /** The caret's position in the text `getText` returns — chips and line breaks
      counted the same way — or the end of the text when the caret is elsewhere.
      Lets a caller insert at the caret without reaching into the DOM. */
  export function caret(): number {
    const live = window.getSelection();
    const active = live?.rangeCount ? live.getRangeAt(0) : null;
    const range = active && editor?.contains(active.startContainer)
      ? active
      : savedRange && editor?.contains(savedRange.startContainer)
        ? savedRange
        : null;
    if (!range) return getText().length;

    let offset = 0;
    let done = false;
    const visit = (node: Node): void => {
      if (done) return;
      if (node.nodeType === Node.TEXT_NODE) {
        if (node === range.startContainer) {
          offset += range.startOffset;
          done = true;
        } else offset += (node.textContent ?? '').length;
        return;
      }
      if (node.nodeType !== Node.ELEMENT_NODE) return;
      const element = node as HTMLElement;
      if (element.hasAttribute('data-chip')) return;
      if (element.tagName === 'BR') {
        offset += 1;
        return;
      }
      // A caret between two nodes reports their parent as its container, with
      // the child index as the offset.
      const children = [...element.childNodes];
      const limit = node === range.startContainer ? range.startOffset : children.length;
      for (let index = 0; index < limit && !done; index += 1) visit(children[index]);
      if (node === range.startContainer) done = true;
      else if (element.tagName === 'DIV' || element.tagName === 'P') offset += 1;
    };

    const children = [...editor.childNodes];
    const limit = editor === range.startContainer ? range.startOffset : children.length;
    for (let index = 0; index < limit && !done; index += 1) visit(children[index]);
    return offset;
  }

  /**
   * A chip is the shared FileAttachment component mounted into the
   * contenteditable, so the composer's attachments and the ones in a sent
   * message are literally the same control rather than two lookalikes.
   */
  function paintChip(element: HTMLElement, chip: InlineChipItem): void {
    const status = chip.status ?? 'done';
    element.dataset.id = chip.id;
    element.dataset.localId = chip.localId;
    element.dataset.name = chip.name;
    element.dataset.status = status;
    element.dataset.progress = String(chip.progress ?? 0);

    const existing = chipComponents.get(element);
    const props = {
      name: chip.name,
      status: status === 'pending' ? ('uploading' as const) : (status as 'uploading' | 'done' | 'error'),
      progress: chip.progress ?? 100,
      halfRow: chips.length > 1,
      onRemove: () => onRemove(chip.id),
    };
    if (existing) {
      Object.assign(existing, props);
      return;
    }
    chipComponents.set(element, mount(FileAttachment, {target: element, props}) as Record<string, unknown>);
  }

  function createChip(chip: InlineChipItem): HTMLElement {
    const element = document.createElement('span');
    element.dataset.chip = '';
    element.contentEditable = 'false';
    element.className = 'inline-chip-wrapper';
    paintChip(element, chip);
    return element;
  }

  function destroyChip(element: HTMLElement): void {
    const component = chipComponents.get(element);
    if (component) void unmount(component);
    chipComponents.delete(element);
    element.remove();
  }

  function insertChip(chip: HTMLElement): void {
    if (savedRange && editor.contains(savedRange.startContainer)) {
      savedRange.deleteContents();
      savedRange.insertNode(chip);
      const after = document.createRange();
      after.setStartAfter(chip);
      after.collapse(true);
      apply(after);
    } else {
      editor.appendChild(chip);
      placeCursorAtEnd();
    }
  }

  function reconcileChips(): void {
    if (!editor || !mounted) return;
    const wanted = new Map(chips.map((chip) => [chip.localId, chip]));

    for (const element of editor.querySelectorAll<HTMLElement>('[data-chip]')) {
      const chip = wanted.get(element.dataset.localId ?? '');
      if (chip) paintChip(element, chip);
      else destroyChip(element);
    }

    for (const chip of chips) {
      if (!editor.querySelector(`[data-chip][data-local-id="${escapeSelector(chip.localId)}"]`)) {
        insertChip(createChip(chip));
      }
    }

    syncEmpty();
    updateExpanded();
  }

  function input(): void {
    if (suppressInput) return;
    const text = getText();
    if (text !== lastEmittedText) {
      lastEmittedText = text;
      onChange(text);
    }

    const present = new Set(
      Array.from(editor.querySelectorAll<HTMLElement>('[data-chip]')).map((chip) => chip.dataset.localId),
    );
    for (const chip of chips) if (!present.has(chip.localId)) onRemove(chip.id);
    syncEmpty();
    updateExpanded();
    editor.scrollTop = editor.scrollHeight;
  }

  function keydown(event: KeyboardEvent): void {
    if (!disabled && event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      submit(event.metaKey || event.ctrlKey);
    }
  }

  function paste(event: ClipboardEvent): void {
    const text = event.clipboardData?.getData('text/plain') ?? '';
    if (!text) return;
    event.preventDefault();

    const selection = window.getSelection();
    if (!selection?.rangeCount) return;
    const range = selection.getRangeAt(0);
    range.deleteContents();
    const node = document.createTextNode(text);
    range.insertNode(node);
    range.setStartAfter(node);
    range.collapse(true);
    selection.removeAllRanges();
    selection.addRange(range);
    savedRange = range.cloneRange();
    input();
  }

  export function clear(): void {
    if (!editor) return;
    suppressInput = true;
    editor.replaceChildren();
    suppressInput = false;
    lastEmittedText = '';
    savedRange = null;
    onChange('');
    syncEmpty();
    updateExpanded();
  }

  /** @param caretAt Where to leave the caret, as an offset into `text`.
      Defaults to the end, which is what typing-like callers want. */
  export function setText(text: string, caretAt?: number): void {
    suppressInput = true;
    editor.replaceChildren();
    if (text) editor.appendChild(document.createTextNode(text));
    suppressInput = false;
    lastEmittedText = text;
    savedRange = null;
    onChange(text);
    reconcileChips();
    editor.focus();
    if (caretAt === undefined) placeCursorAtEnd();
    else placeCursorAt(caretAt);
    syncEmpty();
    updateExpanded();
    editor.scrollTop = editor.scrollHeight;
  }

  export function submit(immediate = false): void {
    const result = serialize();
    onSubmit(result.text, result.chips, immediate);
  }

  onMount(() => {
    if (value) editor.appendChild(document.createTextNode(value));
    lastEmittedText = value;
    mounted = true;
    // macOS Accessibility writes directly into a contenteditable without
    // dispatching the browser's `input` event. Observe those DOM changes so
    // assistive tools update the same composer state as typing does.
    accessibilityObserver = new MutationObserver(input);
    accessibilityObserver.observe(editor, {
      childList: true,
      subtree: true,
      characterData: true,
    });
    reconcileChips();
    syncEmpty();
    updateExpanded();
  });

  afterUpdate(reconcileChips);

  onDestroy(() => {
    accessibilityObserver?.disconnect();
    for (const component of chipComponents.values()) void unmount(component);
    chipComponents.clear();
  });
</script>

<div class="inline-editor-wrap">
  <div
    bind:this={editor}
    class:disabled
    class="inline-chip-editor"
    style:max-height={`calc(${maxLines} * 1.5em)`}
    contenteditable={!disabled}
    tabindex={disabled ? -1 : 0}
    role="textbox"
    aria-label={ariaLabel}
    aria-multiline="true"
    aria-disabled={disabled || undefined}
    data-empty={isEmpty || undefined}
    data-placeholder={placeholder}
    oninput={input}
    onkeydown={keydown}
    onpaste={paste}
    onfocus={rememberSelection}
    onblur={rememberSelection}
  ></div>
</div>
