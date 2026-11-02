import test from 'node:test';
import assert from 'node:assert/strict';
import {
  defaultChatListPreferences,
  filterChatList,
  sortChatList,
  type ChatListEntry,
} from './chatListPreferences';

test('chat lists group by folder by default', () => {
  assert.equal(defaultChatListPreferences.groupBy, 'folder');
});

const chats: ChatListEntry[] = [
  {id: 'alpha', title: 'Alpha', createdAt: 30, updatedAt: 100, running: false},
  {id: 'beta', title: 'Beta', createdAt: 10, updatedAt: 300, running: true},
  {id: 'gamma', title: 'Gamma', createdAt: 20, updatedAt: 200},
];

test('chat list sorting supports activity, creation date, and title in both directions', () => {
  assert.deepEqual(sortChatList(chats, 'activity', 'descending').map(({id}) => id), ['beta', 'gamma', 'alpha']);
  assert.deepEqual(sortChatList(chats, 'created', 'ascending').map(({id}) => id), ['beta', 'gamma', 'alpha']);
  assert.deepEqual(sortChatList(chats, 'name', 'descending').map(({id}) => id), ['gamma', 'beta', 'alpha']);
});

test('chat list filters distinguish activity and folder membership', () => {
  const foldered = new Set(['alpha', 'gamma']);
  assert.deepEqual(filterChatList(chats, 'running', foldered).map(({id}) => id), ['beta']);
  assert.deepEqual(filterChatList(chats, 'idle', foldered).map(({id}) => id), ['alpha', 'gamma']);
  assert.deepEqual(filterChatList(chats, 'foldered', foldered).map(({id}) => id), ['alpha', 'gamma']);
  assert.deepEqual(filterChatList(chats, 'unfiled', foldered).map(({id}) => id), ['beta']);
});
