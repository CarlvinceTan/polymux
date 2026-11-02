const STORAGE_KEY = 'polymuxChatFolders';

export type ChatFolder = {
  id: string;
  name: string;
  collapsed: boolean;
  chatIds: string[];
};

function strings(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === 'string' && Boolean(item))
    : [];
}

/**
 * A chat can belong to one folder only. Normalising on read also makes stale or
 * hand-edited local data harmless, without introducing a nested-folder shape.
 */
function normalise(value: unknown): ChatFolder[] {
  if (!Array.isArray(value)) return [];
  const folderIds = new Set<string>();
  const assignedChats = new Set<string>();
  const folders: ChatFolder[] = [];

  for (const item of value) {
    if (!item || typeof item !== 'object') continue;
    const stored = item as Partial<ChatFolder>;
    const id = typeof stored.id === 'string' ? stored.id : '';
    const name = typeof stored.name === 'string' ? stored.name.trim() : '';
    if (!id || !name || folderIds.has(id)) continue;
    folderIds.add(id);
    const chatIds = strings(stored.chatIds).filter((chatId) => {
      if (assignedChats.has(chatId)) return false;
      assignedChats.add(chatId);
      return true;
    });
    folders.push({id, name, collapsed: stored.collapsed === true, chatIds});
  }
  return folders;
}

function persist(folders: ChatFolder[]): ChatFolder[] {
  if (typeof localStorage === 'undefined') return folders;
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(folders));
  } catch {
    // A denied or full store costs the arrangement, not the chat list.
  }
  return folders;
}

export function loadChatFolders(): ChatFolder[] {
  if (typeof localStorage === 'undefined') return [];
  try {
    return normalise(JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '[]'));
  } catch {
    return [];
  }
}

/**
 * The name a new-folder field opens on: the suggestion, then
 * "New folder 1", "New folder 2", and so on once that name is taken.
 * The suffix counts extra copies rather than numbering the first folder.
 */
export function uniqueChatFolderName(folders: ChatFolder[], name: string): string {
  const suggestion = name.trim();
  if (!suggestion) return suggestion;
  const used = new Set(folders.map((folder) => folder.name.toLowerCase()));
  if (!used.has(suggestion.toLowerCase())) return suggestion;
  for (let index = 1; ; index += 1) {
    const candidate = `${suggestion} ${index}`;
    if (!used.has(candidate.toLowerCase())) return candidate;
  }
}

export function createChatFolder(folders: ChatFolder[], id: string, name: string): ChatFolder[] {
  const cleanName = name.trim();
  if (!id || !cleanName || folders.some((folder) => folder.id === id)) return folders;
  return persist([...folders, {id, name: cleanName, collapsed: false, chatIds: []}]);
}

export function renameChatFolder(folders: ChatFolder[], id: string, name: string): ChatFolder[] {
  const cleanName = name.trim();
  if (!cleanName) return folders;
  return persist(folders.map((folder) => folder.id === id ? {...folder, name: cleanName} : folder));
}

export function deleteChatFolder(folders: ChatFolder[], id: string): ChatFolder[] {
  return persist(folders.filter((folder) => folder.id !== id));
}

export function toggleChatFolder(folders: ChatFolder[], id: string): ChatFolder[] {
  return persist(folders.map((folder) => folder.id === id ? {...folder, collapsed: !folder.collapsed} : folder));
}

export function moveChatToFolder(folders: ChatFolder[], chatId: string, folderId: string | null): ChatFolder[] {
  if (folderId && !folders.some((folder) => folder.id === folderId)) return folders;
  return persist(folders.map((folder) => ({
    ...folder,
    chatIds: folder.id === folderId
      ? [...folder.chatIds.filter((id) => id !== chatId), chatId]
      : folder.chatIds.filter((id) => id !== chatId),
  })));
}

export function folderForChat(folders: ChatFolder[], chatId: string): ChatFolder | undefined {
  return folders.find((folder) => folder.chatIds.includes(chatId));
}

export const chatFoldersStorageKey = STORAGE_KEY;
