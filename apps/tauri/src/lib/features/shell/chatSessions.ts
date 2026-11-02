export interface ChatSnapshot {
  id: string;
  title: string;
  /** Milliseconds since the epoch. */
  updatedAt: number;
}

export type ChatHistoryGroupLabel =
  | 'Today' | 'Yesterday' | 'This week' | 'Last week' | 'This month' | 'Last month' | 'Earlier';

export interface ChatHistoryGroup {
  label: ChatHistoryGroupLabel;
  chats: ChatSnapshot[];
}

const historyGroupOrder: ChatHistoryGroupLabel[] = ['Today', 'Yesterday', 'This week', 'Last week', 'This month', 'Last month', 'Earlier'];

function startOfDay(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate());
}

function startOfWeek(date: Date): Date {
  const day = startOfDay(date);
  const mondayOffset = (day.getDay() + 6) % 7;
  day.setDate(day.getDate() - mondayOffset);
  return day;
}

export function sortChatsNewestFirst<T extends ChatSnapshot>(chats: T[]): T[] {
  return [...chats].sort((a, b) => {
    const right = Number.isFinite(b.updatedAt) ? b.updatedAt : 0;
    const left = Number.isFinite(a.updatedAt) ? a.updatedAt : 0;
    return right - left;
  });
}

export function groupChatsByRecency<T extends ChatSnapshot>(chats: T[], now = new Date()): Array<{label: ChatHistoryGroupLabel; chats: T[]}> {
  const today = startOfDay(now);
  const yesterday = new Date(today);
  yesterday.setDate(yesterday.getDate() - 1);
  const thisWeek = startOfWeek(now);
  const lastWeek = new Date(thisWeek);
  lastWeek.setDate(lastWeek.getDate() - 7);
  const thisMonth = new Date(now.getFullYear(), now.getMonth(), 1);
  const lastMonth = new Date(now.getFullYear(), now.getMonth() - 1, 1);
  const groups = new Map(historyGroupOrder.map((label) => [label, [] as T[]]));

  for (const chat of sortChatsNewestFirst(chats)) {
    const date = Number.isFinite(chat.updatedAt) ? new Date(chat.updatedAt) : null;
    const label: ChatHistoryGroupLabel = !date ? 'Earlier'
      : date >= today ? 'Today'
        : date >= yesterday ? 'Yesterday'
          : date >= thisWeek ? 'This week'
            : date >= lastWeek ? 'Last week'
              : date >= thisMonth ? 'This month'
                : date >= lastMonth ? 'Last month'
                  : 'Earlier';
    groups.get(label)?.push(chat);
  }

  return historyGroupOrder.flatMap((label) => {
    const groupedChats = groups.get(label) ?? [];
    return groupedChats.length ? [{label, chats: groupedChats}] : [];
  });
}
