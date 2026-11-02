import {polymuxApi} from '../../api/polymux';
import type {Platform} from '../components/PlatformLogo.svelte';

/**
 * Which platform each conversation belongs to, so a row that names only a chat
 * id can still be drawn with that platform's logo.
 *
 * The agent's messaging tools take a `chat_id` and nothing else — the platform
 * is the hub's knowledge, not the call's. The activity trail is built
 * synchronously as each call starts, so the answer has to be here already:
 * whatever the hub loads is remembered as it loads, and a run that reaches a
 * conversation before the hub was ever opened primes this from the same list
 * the hub would have fetched.
 */
const platforms = new Map<string, string>();
let priming: Promise<void> | null = null;

/** Every platform the app has a mark for, as a runtime set — the type alone
 * cannot answer whether a platform named by a tool call can be drawn. */
export const PLATFORM_LOGOS = new Set<string>([
  'whatsapp', 'telegram', 'signal', 'slack', 'messenger', 'instagram',
  'linkedin', 'googlechat', 'gmessages', 'twitter', 'bluesky', 'gvoice',
  'zulip', 'imessage', 'wechat', 'matrix', 'mail',
]);

/** Records what a list of conversations says about its platforms. Anything
 * without both an id and a platform is skipped rather than remembered wrong. */
export function rememberChatPlatforms(chats: Iterable<{id?: unknown; platform?: unknown}>): void {
  for (const chat of chats) {
    const id = typeof chat.id === 'string' ? chat.id : '';
    const platform = typeof chat.platform === 'string' ? chat.platform : '';
    if (id && platform) platforms.set(id, platform);
  }
}

/** The platform a chat belongs to, when it is one that can be drawn. */
export function platformForChat(chatId: string): Platform | undefined {
  return asPlatform(platforms.get(chatId));
}

/** A platform name kept only when it is one the app can draw a mark for. */
export function asPlatform(value: unknown): Platform | undefined {
  return typeof value === 'string' && PLATFORM_LOGOS.has(value) ? value as Platform : undefined;
}

/**
 * Fills the map from the hub's own chat list, once per session. Fire and
 * forget: the row being built now takes whatever is already known, and the
 * answer this fetches is what the row picks up when it lands.
 */
export function primeChatPlatforms(): Promise<void> {
  priming ??= (async () => {
    try {
      rememberChatPlatforms(await polymuxApi().comms.chats());
    } catch {
      // No hub, no logos — the trail falls back to its generic glyph.
    }
  })();
  return priming;
}
