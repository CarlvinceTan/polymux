import type {TeamAvatarExpression, BotStatus} from '@polymux/protocol';

export type BloubActivity = 'idle' | 'thinking' | 'working' | 'waiting' | 'attention' | 'complete' | 'failed';

type ExpressionRule = readonly [TeamAvatarExpression, RegExp];

/**
 * Pure presentation rules for Bloub's pose. They inspect display text only and
 * never feed an expression back into prompts, tools, routing, or persistence.
 */
const CHAT_RULES: readonly ExpressionRule[] = [
  ['laughing', /(?:\bhaha\b|\bhehe\b|\blol\b|😂)/i],
  ['frightened', /(?:\burgent\b|\bcritical\b|\bdanger\b|security incident|\bimmediately\b)/i],
  ['angry', /(?:\bblocked\b|\bunacceptable\b|\brefused\b|cannot proceed)/i],
  ['sad', /(?:\bsorry\b|\bfailed\b|\bfailure\b|could(?:n['’]t| not)|\bunable to\b)/i],
  ['surprised', /(?:\bunexpected\b|\bsurpris\w*\b|turns out)/i],
  ['excited', /(?:\bexciting\b|\bamazing\b|great news|!{2,})/i],
  ['proud', /(?:\bcompleted\b|\bdone\b|\bshipped\b|\bfinished\b|\bsuccess(?:ful(?:ly)?)?\b)/i],
  ['happy', /(?:\bthanks\b|thank you|\bglad\b|\bgreat\b|good news)/i],
  ['confused', /(?:\bunclear\b|not sure|\bconfus\w*\b|don['’]t understand)/i],
  ['suspicious', /(?:\bverify\b|\bunverified\b|\bclaim\b|\brisk\b|\bsuspicious\b|double-check)/i],
  ['curious', /(?:\?|\bresearch\b|\binvestigat\w*\b|\bexplor\w*\b|\bcompar\w*\b)/i],
  ['shy', /(?:\bperhaps\b|\bmaybe\b|\btentative(?:ly)?\b)/i],
  ['unimpressed', /(?:no change|\bunchanged\b|\bhowever\b|\bbut\b)/i],
  ['sleepy', /(?:\bwaiting\b|\bidle\b|\blater\b)/i],
  ['attentive', /(?:\breview\w*\b|\bcheck\w*\b|\bworking\b|looking into)/i],
] as const;

export function bloubExpressionForText(text: string): TeamAvatarExpression {
  return CHAT_RULES.find(([, pattern]) => pattern.test(text))?.[0] ?? 'neutral';
}

export function bloubExpressionForTeamStatus(
  status: BotStatus,
  preview = '',
): TeamAvatarExpression {
  if (status === 'working') return 'attentive';
  if (status === 'waiting-for-device') return 'curious';
  if (status === 'computer-offline') return 'sleepy';
  if (status === 'error') return 'sad';
  return bloubExpressionForText(preview);
}

/**
 * One shared status-to-motion map keeps an Agent's pose and movement in sync
 * everywhere it appears: the drawer, group stack and empty conversation.
 *
 * Only a run happening now draws the ring. Waiting, offline and failed are
 * resting states: the pose carries them and the surfaces that need more show a
 * status dot or detail text, so the avatar does not wear an alarm halo.
 */
export function bloubActivityForTeamStatus(status: BotStatus): BloubActivity {
  return status === 'working' ? 'working' : 'idle';
}

export function bloubExpressionForMessage(
  message: Readonly<{text: string; streaming?: boolean}>,
): TeamAvatarExpression {
  return message.streaming ? 'attentive' : bloubExpressionForText(message.text);
}
