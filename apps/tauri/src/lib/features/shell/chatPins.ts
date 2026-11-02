export const chatPinsStorageKey = 'polymuxChatPins';
export type ChatPins = {chats: string[]; folders: string[]};

export function loadChatPins(): ChatPins {
  try {
    const value = JSON.parse(localStorage.getItem(chatPinsStorageKey) ?? 'null');
    const ids = (items: unknown): string[] => Array.isArray(items)
      ? [...new Set(items.filter((id): id is string => typeof id === 'string' && Boolean(id)))] : [];
    return {chats: ids(value?.chats), folders: ids(value?.folders)};
  } catch { return {chats: [], folders: []}; }
}

export function toggleChatPin(pins: ChatPins, kind: keyof ChatPins, id: string): ChatPins {
  const next = {...pins, [kind]: pins[kind].includes(id)
    ? pins[kind].filter((value) => value !== id) : [...pins[kind], id]};
  try { localStorage.setItem(chatPinsStorageKey, JSON.stringify(next)); } catch { /* Keep usable in memory. */ }
  return next;
}
