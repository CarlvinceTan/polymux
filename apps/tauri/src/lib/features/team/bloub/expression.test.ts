import assert from 'node:assert/strict';
import test from 'node:test';
import {TEAM_AVATAR_EXPRESSIONS} from '@polymux/protocol';
import {
  bloubActivityForTeamStatus,
  bloubExpressionForMessage,
  bloubExpressionForTeamStatus,
  bloubExpressionForText,
} from './expression';

test('every Bloub expression is reachable from deterministic display context', () => {
  const examples = new Map([
    ['neutral', 'A plain status update.'],
    ['attentive', 'Reviewing the brief.'],
    ['surprised', 'That result was unexpected.'],
    ['excited', 'This is amazing!!'],
    ['happy', 'Thanks, this looks great.'],
    ['laughing', 'Haha, that worked.'],
    ['angry', 'The request was refused.'],
    ['sad', 'Sorry, the run failed.'],
    ['frightened', 'Critical security incident.'],
    ['suspicious', 'We should verify that claim.'],
    ['confused', 'The requirement is unclear.'],
    ['curious', 'Could I investigate this?'],
    ['proud', 'The work is completed.'],
    ['shy', 'Perhaps this is the right approach.'],
    ['unimpressed', 'There was no change.'],
    ['sleepy', 'Waiting until later.'],
  ] as const);

  for (const expression of TEAM_AVATAR_EXPRESSIONS) {
    const text = examples.get(expression);
    assert.ok(text, `missing example for ${expression}`);
    assert.equal(bloubExpressionForText(text), expression);
    assert.equal(bloubExpressionForText(text), expression);
  }
});

test('live Team status takes precedence over preview wording', () => {
  assert.equal(bloubExpressionForTeamStatus('working', 'Sorry, failed.'), 'attentive');
  assert.equal(bloubExpressionForTeamStatus('waiting-for-device'), 'curious');
  assert.equal(bloubExpressionForTeamStatus('computer-offline'), 'sleepy');
  assert.equal(bloubExpressionForTeamStatus('error'), 'sad');
  assert.equal(bloubExpressionForTeamStatus('idle', 'The work is completed.'), 'proud');
});

test('maps durable team state to motion, ringing only for live work', () => {
  assert.equal(bloubActivityForTeamStatus('idle'), 'idle');
  assert.equal(bloubActivityForTeamStatus('working'), 'working');
  assert.equal(bloubActivityForTeamStatus('waiting-for-device'), 'idle');
  assert.equal(bloubActivityForTeamStatus('computer-offline'), 'idle');
  assert.equal(bloubActivityForTeamStatus('error'), 'idle');
});

test('streaming is attentive and message inputs are not mutated', () => {
  const message = Object.freeze({text: 'The work is completed.', streaming: true});
  assert.equal(bloubExpressionForMessage(message), 'attentive');
  assert.deepEqual(message, {text: 'The work is completed.', streaming: true});
  assert.equal(bloubExpressionForMessage({text: message.text}), 'proud');
});
