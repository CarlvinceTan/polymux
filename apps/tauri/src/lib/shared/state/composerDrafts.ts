import type {MailImportance} from '@polymux/protocol';

const STORAGE_KEY = 'polymux.composer-drafts.v1';

export type ChatComposerDraft = {
  text: string;
  replyTo: string | null;
  files: string[];
};

export type MailComposerInlineAttachment = {
  path: string;
  contentId: string;
  offset: number;
};

export type MailComposerDraft = {
  localId: string;
  revision: number;
  pending: boolean;
  account: string;
  folder: string;
  to: string;
  cc: string;
  bcc: string;
  subject: string;
  body: string;
  /** Kept separate so changing a preset never rewrites authored text. */
  signatureId: string;
  signatureBody: string;
  signatureHtml: string | null;
  files: string[];
  /** Files inserted at a body caret. Paths absent here remain trailing MIME
   * attachments even though the composer still shows them after the text. */
  inlineFiles: MailComposerInlineAttachment[];
  importance: MailImportance;
  reply: {inReplyTo: string | null; references: string[]} | null;
  remoteDraft: {id: string; folder: string} | null;
};

type ComposerDraftStore = {
  agent: Record<string, string>;
  chat: Record<string, ChatComposerDraft>;
  mail: Record<string, MailComposerDraft>;
};

const EMPTY = (): ComposerDraftStore => ({agent: {}, chat: {}, mail: {}});

function storage(): Storage | null {
  return typeof localStorage === 'undefined' ? null : localStorage;
}

function object(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

function read(): ComposerDraftStore {
  const target = storage();
  if (!target) return EMPTY();
  try {
    const raw: unknown = JSON.parse(target.getItem(STORAGE_KEY) ?? '{}');
    if (!object(raw)) return EMPTY();
    return {
      agent: object(raw.agent)
        ? Object.fromEntries(Object.entries(raw.agent).filter((entry): entry is [string, string] => typeof entry[1] === 'string'))
        : {},
      chat: object(raw.chat) ? raw.chat as Record<string, ChatComposerDraft> : {},
      mail: object(raw.mail) ? raw.mail as Record<string, MailComposerDraft> : {},
    };
  } catch {
    return EMPTY();
  }
}

function write(next: ComposerDraftStore): void {
  const target = storage();
  if (!target) return;
  try {
    target.setItem(STORAGE_KEY, JSON.stringify(next));
  } catch {
    // A full or unavailable renderer store should never stop the composer.
  }
}

export function newAgentDraftKey(folderId: string | null): string {
  return folderId ? `new:folder:${folderId}` : 'new';
}

export function loadAgentDraft(key: string): string {
  return read().agent[key] ?? '';
}

export function saveAgentDraft(key: string, text: string): void {
  if (!key) return;
  const next = read();
  if (text) next.agent[key] = text;
  else delete next.agent[key];
  write(next);
}

export function loadChatDraft(chatId: string): ChatComposerDraft {
  const saved = read().chat[chatId];
  return saved && typeof saved.text === 'string'
    ? {
        text: saved.text,
        replyTo: typeof saved.replyTo === 'string' ? saved.replyTo : null,
        files: Array.isArray(saved.files)
          ? saved.files.filter((item): item is string => typeof item === 'string' && Boolean(item))
          : [],
      }
    : {text: '', replyTo: null, files: []};
}

export function saveChatDraft(chatId: string, draft: ChatComposerDraft): void {
  if (!chatId) return;
  const next = read();
  if (draft.text || draft.replyTo || draft.files.length > 0) next.chat[chatId] = draft;
  else delete next.chat[chatId];
  write(next);
}

export function loadMailDraft(account: string): MailComposerDraft | null {
  const saved = read().mail[account];
  if (!saved || typeof saved.localId !== 'string' || saved.account !== account) return null;
  const body = typeof saved.body === 'string' ? saved.body : '';
  const files = Array.isArray(saved.files)
    ? saved.files.filter((item): item is string => typeof item === 'string')
    : [];
  const fileSet = new Set(files);
  return {
    localId: saved.localId,
    revision: Number.isFinite(saved.revision) ? saved.revision : 0,
    pending: saved.pending === true,
    account,
    folder: typeof saved.folder === 'string' ? saved.folder : 'INBOX',
    to: typeof saved.to === 'string' ? saved.to : '',
    cc: typeof saved.cc === 'string' ? saved.cc : '',
    bcc: typeof saved.bcc === 'string' ? saved.bcc : '',
    subject: typeof saved.subject === 'string' ? saved.subject : '',
    body,
    signatureId: typeof saved.signatureId === 'string' ? saved.signatureId : '',
    signatureBody: typeof saved.signatureBody === 'string' ? saved.signatureBody : '',
    signatureHtml: typeof saved.signatureHtml === 'string' ? saved.signatureHtml : null,
    files,
    inlineFiles: Array.isArray(saved.inlineFiles)
      ? saved.inlineFiles.flatMap((item) => {
          if (!item || typeof item !== 'object') return [];
          const value = item as Partial<MailComposerInlineAttachment>;
          if (
            typeof value.path !== 'string' ||
            !fileSet.has(value.path) ||
            typeof value.contentId !== 'string' ||
            !/^[^\s<>]+$/.test(value.contentId) ||
            typeof value.offset !== 'number' ||
            !Number.isFinite(value.offset)
          ) return [];
          return [{
            path: value.path,
            contentId: value.contentId,
            offset: Math.max(0, Math.min(body.length, Math.trunc(value.offset))),
          }];
        })
      : [],
    importance: saved.importance === 'high' || saved.importance === 'low' ? saved.importance : 'normal',
    reply: saved.reply && typeof saved.reply === 'object'
      ? {
          inReplyTo: typeof saved.reply.inReplyTo === 'string' ? saved.reply.inReplyTo : null,
          references: Array.isArray(saved.reply.references)
            ? saved.reply.references.filter((item): item is string => typeof item === 'string')
            : [],
        }
      : null,
    remoteDraft:
      saved.remoteDraft && typeof saved.remoteDraft.id === 'string' && typeof saved.remoteDraft.folder === 'string'
        ? saved.remoteDraft
        : null,
  };
}

export function saveMailDraft(draft: MailComposerDraft): void {
  if (!draft.account) return;
  const next = read();
  next.mail[draft.account] = draft;
  write(next);
}

export function clearMailDraft(account: string, localId?: string): void {
  const next = read();
  if (localId && next.mail[account]?.localId !== localId) return;
  delete next.mail[account];
  write(next);
}
