const STORAGE_KEY = 'polymuxChatListPreferences';

export type ChatListGroupBy = 'date' | 'folder' | 'state' | 'none';
export type ChatListSortBy = 'activity' | 'created' | 'name';
export type ChatListSortOrder = 'descending' | 'ascending';
export type ChatListFilter = 'all' | 'running' | 'idle' | 'foldered' | 'unfiled';

export type ChatListPreferences = {
  groupBy: ChatListGroupBy;
  sortBy: ChatListSortBy;
  order: ChatListSortOrder;
  filter: ChatListFilter;
};

export type ChatListEntry = {
  id: string;
  title: string;
  createdAt: number;
  updatedAt: number;
  running?: boolean;
};

export const defaultChatListPreferences: ChatListPreferences = {
  groupBy: 'folder',
  sortBy: 'activity',
  order: 'descending',
  filter: 'all',
};

const groupByValues = new Set<ChatListGroupBy>(['date', 'folder', 'state', 'none']);
const sortByValues = new Set<ChatListSortBy>(['activity', 'created', 'name']);
const orderValues = new Set<ChatListSortOrder>(['descending', 'ascending']);
const filterValues = new Set<ChatListFilter>(['all', 'running', 'idle', 'foldered', 'unfiled']);
const titleCollator = new Intl.Collator(undefined, {numeric: true, sensitivity: 'base'});

function normalise(value: unknown): ChatListPreferences {
  if (!value || typeof value !== 'object') return {...defaultChatListPreferences};
  const stored = value as Partial<ChatListPreferences>;
  return {
    groupBy: groupByValues.has(stored.groupBy as ChatListGroupBy)
      ? stored.groupBy as ChatListGroupBy
      : defaultChatListPreferences.groupBy,
    sortBy: sortByValues.has(stored.sortBy as ChatListSortBy)
      ? stored.sortBy as ChatListSortBy
      : defaultChatListPreferences.sortBy,
    order: orderValues.has(stored.order as ChatListSortOrder)
      ? stored.order as ChatListSortOrder
      : defaultChatListPreferences.order,
    filter: filterValues.has(stored.filter as ChatListFilter)
      ? stored.filter as ChatListFilter
      : defaultChatListPreferences.filter,
  };
}

export function loadChatListPreferences(): ChatListPreferences {
  if (typeof localStorage === 'undefined') return {...defaultChatListPreferences};
  try {
    return normalise(JSON.parse(localStorage.getItem(STORAGE_KEY) ?? 'null'));
  } catch {
    return {...defaultChatListPreferences};
  }
}

export function saveChatListPreferences(preferences: ChatListPreferences): ChatListPreferences {
  const safe = normalise(preferences);
  if (typeof localStorage !== 'undefined') {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(safe));
    } catch {
      // A denied or full store costs the preference, not the chat list.
    }
  }
  return safe;
}

export function filterChatList<T extends ChatListEntry>(
  chats: T[],
  filter: ChatListFilter,
  folderedChatIds: ReadonlySet<string>,
): T[] {
  if (filter === 'all') return [...chats];
  return chats.filter((chat) => {
    if (filter === 'running') return chat.running === true;
    if (filter === 'idle') return chat.running !== true;
    if (filter === 'foldered') return folderedChatIds.has(chat.id);
    return !folderedChatIds.has(chat.id);
  });
}

function validTime(value: number): number | null {
  return Number.isFinite(value) ? value : null;
}

function compareTimes(left: number, right: number, order: ChatListSortOrder): number {
  const leftTime = validTime(left);
  const rightTime = validTime(right);
  if (leftTime === null && rightTime === null) return 0;
  if (leftTime === null) return 1;
  if (rightTime === null) return -1;
  return order === 'descending' ? rightTime - leftTime : leftTime - rightTime;
}

export function sortChatList<T extends ChatListEntry>(
  chats: T[],
  sortBy: ChatListSortBy,
  order: ChatListSortOrder,
): T[] {
  return [...chats].sort((left, right) => {
    const primary = sortBy === 'name'
      ? titleCollator.compare(left.title, right.title) * (order === 'descending' ? -1 : 1)
      : compareTimes(
          sortBy === 'created' ? left.createdAt : left.updatedAt,
          sortBy === 'created' ? right.createdAt : right.updatedAt,
          order,
        );
    if (primary) return primary;
    const title = titleCollator.compare(left.title, right.title);
    return title || left.id.localeCompare(right.id);
  });
}

export const chatListPreferencesStorageKey = STORAGE_KEY;
