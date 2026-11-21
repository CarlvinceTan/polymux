import {readFileSync} from 'node:fs';
import assert from 'node:assert/strict';
import test from 'node:test';
import {JSDOM} from 'jsdom';

const page = readFileSync(new URL('../public/connect.html', import.meta.url), 'utf8');

test('pairing page hands the unmodified fragment to the app', () => {
  const payload = 'url=https://server.example&token=a+b/c%3D&ticket=x%26y';
  const {window} = new JSDOM(page, {url: `https://polymux.com/connect#${payload}`, runScripts: 'dangerously'});
  try {
    const link = window.document.querySelector('#open');
    assert.equal(link.hidden, false);
    assert.equal(link.getAttribute('href'), `polymux://connect?${payload}`);
    assert.equal(window.location.search, '');
  } finally {
    window.close();
  }
});

test('pairing page without a fragment does not offer a broken app link', () => {
  const {window} = new JSDOM(page, {url: 'https://polymux.com/connect', runScripts: 'dangerously'});
  try {
    assert.equal(window.document.querySelector('#open').hidden, true);
  } finally {
    window.close();
  }
});
