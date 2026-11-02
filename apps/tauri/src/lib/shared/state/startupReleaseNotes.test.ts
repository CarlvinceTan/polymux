import assert from 'node:assert/strict';
import test from 'node:test';
import {startupReleaseNotes} from './startupReleaseNotes';

class MemoryStorage {
  private readonly values = new Map<string, string>();

  getItem(key: string): string | null {
    return this.values.get(key) ?? null;
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value);
  }
}

test('fresh installs establish a version baseline without opening release notes', () => {
  const storage = new MemoryStorage();
  assert.equal(startupReleaseNotes(storage, '0.2.2', false), null);
  assert.equal(startupReleaseNotes(storage, '0.2.2', true), null);
});

test('existing installs see their versioned release page once', () => {
  const storage = new MemoryStorage();
  const destination = startupReleaseNotes(storage, '0.2.2', true);

  assert.deepEqual(destination, {
    version: '0.2.2',
    title: 'Polymux 0.2.2 release notes',
    url: 'https://polymux.com/releases/0.2.2/',
  });
  assert.equal(startupReleaseNotes(storage, '0.2.2', true), null);
});

test('a later version opens once, while a downgrade stays quiet', () => {
  const storage = new MemoryStorage();
  startupReleaseNotes(storage, '0.2.1', false);

  assert.equal(startupReleaseNotes(storage, 'v0.2.2', true)?.version, '0.2.2');
  assert.equal(startupReleaseNotes(storage, '0.2.2', true), null);
  assert.equal(startupReleaseNotes(storage, '0.2.1', true), null);
});
