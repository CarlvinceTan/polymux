import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {mkdtempSync, mkdirSync, readFileSync, rmSync, symlinkSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import test from 'node:test';

const script = readFileSync(new URL('../public/install-cli.sh', import.meta.url), 'utf8');

test('CLI bootstrap does not replace a managed executable or symlink', {skip: process.platform !== 'darwin' || process.arch !== 'arm64'}, () => {
  const home = mkdtempSync(join(tmpdir(), 'polymux-cli-'));
  try {
    const bin = join(home, '.local', 'bin');
    const destination = join(bin, 'polymux');
    const original = join(home, 'managed');
    mkdirSync(bin, {recursive: true});
    writeFileSync(original, 'existing executable');
    for (const symlink of [false, true]) {
      if (symlink) symlinkSync(original, destination);
      else writeFileSync(destination, 'existing executable');
      const result = spawnSync('/bin/sh', ['-s'], {input: script, encoding: 'utf8', env: {...process.env, HOME: home}});
      assert.equal(result.status, 1);
      assert.match(result.stderr, /already installed/);
      assert.equal(readFileSync(destination, 'utf8'), 'existing executable');
      rmSync(destination);
    }
  } finally {
    rmSync(home, {recursive: true});
  }
});
