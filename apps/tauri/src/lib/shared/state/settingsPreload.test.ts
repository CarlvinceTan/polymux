import assert from 'node:assert/strict';
import test from 'node:test';
import type {PolymuxApi} from '@polymux/protocol';
import {
  preloadSettings,
  settingsBrowserSnapshot,
  settingsDriveSnapshot,
  settingsHubSnapshot,
  settingsSnapshot,
} from './settingsPreload.js';

test('startup Settings warm fills every first-open cache without duplicate reads', async () => {
  const calls = new Map<string, number>();
  const answer = <T>(name: string, value: T): Promise<T> => {
    calls.set(name, (calls.get(name) ?? 0) + 1);
    return Promise.resolve(value);
  };
  let releaseGeneral!: () => void;
  const generalGate = new Promise<void>((resolve) => releaseGeneral = resolve);
  const general = {theme: 'system', language: 'system', pinnedViews: []};
  const hub = {bridges: [], email: {accounts: [], signInProviders: []}};
  const drive = {providers: [], sources: [], saveOrder: []};
  const browser = {downloadDirectory: '/tmp'};
  const profiles = {activeId: 'default', profiles: [{id: 'default', name: 'Default Profile', isDefault: true}]};

  const api = {
    mcp: {list: () => answer('mcp', [])},
    skills: {list: () => answer('skills', [])},
    plugins: {list: () => answer('plugins', [])},
    apps: {list: () => answer('apps', {apps: [], pinnedIds: []})},
    models: {
      list: () => answer('models', []),
      metadata: () => answer('modelMetadata', {}),
      roles: () => answer('modelRoles', {}),
    },
    providers: {list: () => answer('providers', [])},
    memory: {
      status: () => answer('memoryStatus', {enabled: true}),
      entries: () => answer('memoryEntries', []),
    },
    computerHistory: {
      status: () => answer('historyStatus', {enabled: true}),
      entries: () => answer('historyEntries', []),
    },
    general: {
      get: async () => {
        calls.set('general', (calls.get('general') ?? 0) + 1);
        await generalGate;
        return general;
      },
      version: () => answer('version', {version: '0.2.4', packaged: false}),
    },
    extension: {status: () => answer('extension', {installed: true})},
    agentRuntime: {
      get: () => answer('agentRuntime', {kind: 'polymux', name: 'Polymux Agent'}),
      registry: () => {
        calls.set('agentRegistry', (calls.get('agentRegistry') ?? 0) + 1);
        return Promise.reject(new Error('registry offline'));
      },
    },
    profiles: {list: () => answer('profiles', profiles)},
    comms: {status: () => answer('hub', hub)},
    drive: {status: () => answer('drive', drive)},
    browser: {
      settings: () => answer('browserSettings', browser),
      logins: () => answer('browserLogins', []),
      downloads: () => answer('browserDownloads', []),
      permissions: () => answer('browserPermissions', []),
    },
  } as unknown as PolymuxApi;

  const first = preloadSettings(api);
  const second = preloadSettings(api);
  releaseGeneral();
  await Promise.all([first, second]);

  for (const [name, count] of calls)
    assert.equal(count, 1, `${name} should be read once`);
  assert.equal(settingsSnapshot.loaded, true);
  assert.equal(settingsSnapshot.historyLoaded, true);
  assert.equal(settingsSnapshot.detailsLoaded, true);
  assert.deepEqual(settingsSnapshot.acpRegistry, []);
  assert.equal(settingsHubSnapshot.status, hub);
  assert.equal(settingsDriveSnapshot.status, drive);
  assert.equal(settingsBrowserSnapshot.settings, browser);
});
