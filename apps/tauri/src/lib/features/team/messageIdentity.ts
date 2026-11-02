import type {AgentMessageOriginDto, TeamAvatarDto} from '@polymux/protocol';

export type TeamMessageLike = {
  role: 'user' | 'assistant';
  origin?: AgentMessageOriginDto;
};

export type TeamMessageSpeaker = {
  side: 'human' | 'agent';
  key: string;
  name: string;
  role: string | null;
  avatar: TeamAvatarDto | null;
  source: 'human' | 'member' | 'peer';
};

export type TeamMessageContext = {
  id: string;
  conversationId: string;
  name: string;
  role: string;
  avatar: TeamAvatarDto | null;
};

/**
 * Team history stores an agent-to-agent relay with a user-shaped transport
 * role so it can become the recipient agent's input. The UI must use durable
 * provenance, rather than that transport role, to decide who visibly spoke.
 */
export function teamMessageSpeaker(
  message: TeamMessageLike,
  member: TeamMessageContext,
): TeamMessageSpeaker {
  if (message.origin) {
    const origin = message.origin;
    return {
      side: 'agent',
      key: `${origin.kind}:${origin.memberId ?? origin.conversationId}`,
      name: origin.name,
      role: origin.role,
      avatar: origin.avatar,
      source: 'peer',
    };
  }

  if (message.role === 'assistant') {
    return {
      side: 'agent',
      key: `team:${member.id}`,
      name: member.name,
      role: member.role,
      avatar: member.avatar,
      source: 'member',
    };
  }

  return {
    side: 'human',
    key: 'human:self',
    name: 'You',
    role: null,
    avatar: null,
    source: 'human',
  };
}
