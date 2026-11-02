import assert from 'node:assert/strict';
import test from 'node:test';
import type {AgentMessageOriginDto, BotDto} from '@polymux/protocol';
import {teamMessageSpeaker} from './messageIdentity';

const member = {
  id: 'maya',
  conversationId: 'team-maya',
  name: 'Maya',
  role: 'Product researcher',
  avatar: {shape: 'pebble', color: '#7557d3'},
} satisfies Pick<BotDto, 'id' | 'conversationId' | 'name' | 'role' | 'avatar'>;

const peer = {
  kind: 'team',
  memberId: 'linus',
  conversationId: 'team-linus',
  name: 'Linus',
  role: 'Engineer',
  avatar: {shape: 'triangle', color: '#2e8b72'},
  traceId: 'trace-1',
  hop: 1,
  automatic: true,
} satisfies AgentMessageOriginDto;

test('human messages stay on the human side', () => {
  assert.deepEqual(teamMessageSpeaker({role: 'user'}, member), {
    side: 'human',
    key: 'human:self',
    name: 'You',
    role: null,
    avatar: null,
    source: 'human',
  });
});

test('the selected bot is the speaker of ordinary assistant output', () => {
  assert.deepEqual(teamMessageSpeaker({role: 'assistant'}, member), {
    side: 'agent',
    key: 'team:maya',
    name: 'Maya',
    role: 'Product researcher',
    avatar: member.avatar,
    source: 'member',
  });
});

test('peer provenance overrides a user-shaped relay transport role', () => {
  assert.deepEqual(teamMessageSpeaker({role: 'user', origin: peer}, member), {
    side: 'agent',
    key: 'team:linus',
    name: 'Linus',
    role: 'Engineer',
    avatar: peer.avatar,
    source: 'peer',
  });
});

test('an avatar-less Assistant relay remains an explicitly named agent', () => {
  const origin = {...peer, kind: 'assistant', memberId: null, avatar: null, name: 'Research notes', role: null} satisfies AgentMessageOriginDto;
  assert.deepEqual(teamMessageSpeaker({role: 'user', origin}, member), {
    side: 'agent',
    key: 'assistant:team-linus',
    name: 'Research notes',
    role: null,
    avatar: null,
    source: 'peer',
  });
});
