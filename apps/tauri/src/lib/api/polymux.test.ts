import assert from 'node:assert/strict';
import test from 'node:test';
import type {ContactLinkDto} from '@polymux/protocol';
import {mergeDemoContactLinkMembers} from './polymux.js';

test('demo contact-link merges retain earlier routes', () => {
  const existing: ContactLinkDto = {
    id: 'contact-1',
    name: 'Friend',
    members: [
      {platform: 'telegram', remoteId: 'tg-1', chatId: '!telegram:local'},
      {platform: 'whatsapp', remoteId: 'wa-1', chatId: '!whatsapp:local'},
    ],
    createdAt: '2026-08-29T00:00:00.000Z',
    updatedAt: '2026-08-29T00:00:00.000Z',
  };

  assert.deepEqual(
    mergeDemoContactLinkMembers(
      [existing],
      [
        {platform: 'telegram', remoteId: 'TG-1', chatId: '!telegram-new:local'},
        {platform: 'wechat', remoteId: 'wxid_friend', chatId: '!wechat:local'},
      ],
    ),
    [
      {platform: 'telegram', remoteId: 'TG-1', chatId: '!telegram-new:local'},
      {platform: 'whatsapp', remoteId: 'wa-1', chatId: '!whatsapp:local'},
      {platform: 'wechat', remoteId: 'wxid_friend', chatId: '!wechat:local'},
    ],
  );
});
