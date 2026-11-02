<script lang="ts">
  import DOMPurify from 'dompurify';
  import Icon from '../../shared/components/Icon.svelte';
  import Menu from '../../shared/components/Menu.svelte';

  export let html: string | null = null;
  export let text = '';
  export let label = 'Signature preview';
  export let placeholder = '';
  export let onChange: (value: {body: string; html: string | null}) => void = () => {};

  const FONTS = [
    {value: 'Helvetica', label: 'Helvetica'},
    {value: 'Arial', label: 'Arial'},
    {value: 'Georgia', label: 'Georgia'},
    {value: 'Times New Roman', label: 'Times New Roman'},
    {value: 'Courier New', label: 'Courier New'},
  ];
  const SIZES = [
    {value: '1', label: '10'},
    {value: '2', label: '12'},
    {value: '3', label: '14'},
    {value: '4', label: '18'},
    {value: '5', label: '24'},
    {value: '6', label: '32'},
  ];

  let editor: HTMLDivElement;
  let focused = false;
  let savedRange: Range | null = null;
  let font = 'Helvetica';
  let size = '3';
  let colour = '#1f1f1f';
  let bold = false;
  let italic = false;
  let underline = false;
  let alignment: 'left' | 'center' | 'right' = 'left';

  $: source = html?.trim() ? html : textToHtml(text);
  $: if (editor && !focused && editor.innerHTML !== source) editor.innerHTML = source;

  function textToHtml(value: string): string {
    return value
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/\n/g, '<br>');
  }

  /** Only the small email-safe subset exposed by this toolbar is persisted. */
  function cleanMarkup(value: string): string {
    const clean = DOMPurify.sanitize(value, {
      ALLOWED_TAGS: ['div', 'p', 'br', 'span', 'b', 'strong', 'i', 'em', 'u', 's', 'strike', 'font', 'a', 'ul', 'ol', 'li'],
      ALLOWED_ATTR: ['style', 'face', 'size', 'color', 'align', 'href'],
      ALLOW_DATA_ATTR: false,
    });
    const holder = document.createElement('div');
    holder.innerHTML = clean;
    const allowedStyles = new Set([
      'color',
      'font-family',
      'font-size',
      'font-style',
      'font-weight',
      'text-align',
      'text-decoration',
      'text-decoration-line',
    ]);
    for (const element of holder.querySelectorAll<HTMLElement>('*')) {
      const kept: string[] = [];
      for (const property of Array.from(element.style)) {
        const value = element.style.getPropertyValue(property).trim();
        if (allowedStyles.has(property) && !/url\s*\(|expression\s*\(|@import/i.test(value))
          kept.push(`${property}: ${value}`);
      }
      if (kept.length) element.setAttribute('style', kept.join('; '));
      else element.removeAttribute('style');
      if (element instanceof HTMLAnchorElement && !/^(https?:|mailto:)/i.test(element.href))
        element.removeAttribute('href');
    }
    return holder.innerHTML;
  }

  function plainText(): string {
    return editor.innerText
      .replace(/\u00a0/g, ' ')
      .replace(/\n{3,}/g, '\n\n')
      .replace(/\n$/, '');
  }

  function commit(): void {
    const clean = cleanMarkup(editor.innerHTML);
    if (clean !== editor.innerHTML) editor.innerHTML = clean;
    const body = plainText();
    onChange({body, html: body ? clean : null});
    rememberSelection();
    readFormatting();
  }

  function rememberSelection(): void {
    if (!editor) return;
    const selection = document.getSelection();
    if (!selection?.rangeCount) return;
    const range = selection.getRangeAt(0);
    if (editor.contains(range.commonAncestorContainer)) savedRange = range.cloneRange();
  }

  function restoreSelection(): void {
    editor.focus();
    if (!savedRange) return;
    const selection = document.getSelection();
    selection?.removeAllRanges();
    selection?.addRange(savedRange);
  }

  function command(name: string, value?: string): void {
    restoreSelection();
    document.execCommand('styleWithCSS', false, 'false');
    document.execCommand(name, false, value);
    commit();
  }

  function readFormatting(): void {
    bold = document.queryCommandState('bold');
    italic = document.queryCommandState('italic');
    underline = document.queryCommandState('underline');
    alignment = document.queryCommandState('justifyCenter')
      ? 'center'
      : document.queryCommandState('justifyRight')
        ? 'right'
        : 'left';
  }

  function chooseFont(value: string): void {
    font = value;
    command('fontName', value);
  }

  function chooseSize(value: string): void {
    size = value;
    command('fontSize', value);
  }

  function chooseColour(event: Event): void {
    colour = (event.currentTarget as HTMLInputElement).value;
    command('foreColor', colour);
  }

  function keepSelection(event: MouseEvent): void {
    event.preventDefault();
  }
</script>

<svelte:document onselectionchange={rememberSelection} />

<div class="signature-rich-editor">
  <div class="signature-rich-toolbar" role="toolbar" aria-label="Signature formatting">
    <Menu options={FONTS} value={font} label="Font" plain onChange={chooseFont} />
    <Menu options={SIZES} value={size} label="Font size" plain onChange={chooseSize} />
    <span class="signature-rich-divider"></span>
    <button type="button" class:active={bold} aria-label="Bold" title="Bold" onmousedown={keepSelection} onclick={() => command('bold')}><b>B</b></button>
    <button type="button" class:active={italic} aria-label="Italic" title="Italic" onmousedown={keepSelection} onclick={() => command('italic')}><i>I</i></button>
    <button type="button" class:active={underline} aria-label="Underline" title="Underline" onmousedown={keepSelection} onclick={() => command('underline')}><u>U</u></button>
    <label class="signature-rich-colour" aria-label="Text colour" title="Text colour" style={`--signature-colour:${colour}`}>
      <span>A</span>
      <input type="color" value={colour} oninput={chooseColour} onmousedown={rememberSelection} />
    </label>
    <span class="signature-rich-divider"></span>
    <button type="button" class:active={alignment === 'left'} aria-label="Align left" title="Align left" onmousedown={keepSelection} onclick={() => command('justifyLeft')}><Icon name="align-left" size={14} /></button>
    <button type="button" class:active={alignment === 'center'} aria-label="Align centre" title="Align centre" onmousedown={keepSelection} onclick={() => command('justifyCenter')}><Icon name="align-center" size={14} /></button>
    <button type="button" class:active={alignment === 'right'} aria-label="Align right" title="Align right" onmousedown={keepSelection} onclick={() => command('justifyRight')}><Icon name="align-right" size={14} /></button>
  </div>
  <div
    bind:this={editor}
    class="signature-rich-surface"
    contenteditable="true"
    tabindex="0"
    role="textbox"
    aria-label={label}
    aria-multiline="true"
    data-placeholder={placeholder}
    spellcheck="true"
    onfocus={() => (focused = true)}
    onblur={() => { focused = false; commit(); }}
    oninput={commit}
    onkeyup={() => { rememberSelection(); readFormatting(); }}
    onmouseup={() => { rememberSelection(); readFormatting(); }}
  ></div>
</div>

<style>
  .signature-rich-editor{min-height:190px;flex:1;display:flex;flex-direction:column;overflow:hidden;border:1px solid var(--neutral-200);border-radius:8px;background:var(--app-surface)}
  .signature-rich-editor:focus-within{border-color:var(--neutral-500)}
  .signature-rich-toolbar{min-height:34px;display:flex;align-items:center;gap:2px;overflow-x:auto;padding:3px 5px;border-bottom:1px solid var(--neutral-200);scrollbar-width:none}
  .signature-rich-toolbar::-webkit-scrollbar{display:none}
  .signature-rich-toolbar :global(.select-menu-trigger){height:25px;gap:4px;padding:0 6px;border-radius:6px;color:var(--neutral-700);font-size:10px}
  .signature-rich-toolbar :global(.select-menu-trigger:hover){background:var(--neutral-100);color:var(--neutral-950)}
  .signature-rich-toolbar :global(.select-menu:first-child .select-menu-trigger){min-width:76px;justify-content:flex-start}
  .signature-rich-toolbar :global(.select-menu:nth-child(2) .select-menu-trigger){min-width:34px;justify-content:center}
  .signature-rich-toolbar>button{width:25px;height:25px;flex:none;display:grid;place-items:center;border:0;border-radius:6px;padding:0;background:transparent;color:var(--neutral-600);cursor:pointer;font-family:inherit;font-size:11px}
  .signature-rich-toolbar>button:hover,.signature-rich-toolbar>button.active{background:var(--neutral-100);color:var(--neutral-950)}
  .signature-rich-divider{width:1px;height:15px;flex:none;margin:0 3px;background:var(--neutral-200)}
  .signature-rich-colour{position:relative;width:25px;height:25px;flex:none;display:flex;align-items:center;justify-content:center;border-radius:6px;color:var(--neutral-700);cursor:pointer;font-size:11px;font-weight:600}
  .signature-rich-colour:hover{background:var(--neutral-100)}
  .signature-rich-colour span{padding-bottom:3px;border-bottom:2px solid var(--signature-colour,currentColor);line-height:12px}
  .signature-rich-colour input{position:absolute;width:1px;height:1px;overflow:hidden;opacity:0}
  .signature-rich-surface{box-sizing:border-box;min-height:150px;flex:1;overflow:auto;padding:11px;color:var(--neutral-950);font-family:Helvetica,Arial,sans-serif;font-size:12px;line-height:1.55;outline:0;white-space:normal}
  .signature-rich-surface:empty::before{color:var(--neutral-400);content:attr(data-placeholder);pointer-events:none}
  .signature-rich-surface :global(p),.signature-rich-surface :global(div){margin:0}
</style>
