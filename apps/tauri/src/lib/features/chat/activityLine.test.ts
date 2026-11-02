import assert from 'node:assert/strict';
import test from 'node:test';
import type { Agent, Message, Run } from '../../api/contract';
import { activeActivities, activityLine, toolPhrase } from './activityLine';

const agent = (id: string, name: string): Agent => ({ id, name, role: '', model: '', active: true });
const run = (patch: Partial<Run>): Run => ({
  jobID: '', id: 'r1', conversationID: 'chat', trigger: 'message', attempt: 1, seq: 1,
  createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z', activity: 'generating',
  agentID: 'a', status: 'running', summary: '', ...patch,
});

test('tool phrases follow the reference mapping and fall back for unknown tools', () => {
  assert.equal(toolPhrase('functions.read_file'), 'Reading the file');
  assert.equal(toolPhrase('mcp__filesystem__read_file'), 'Reading the file');
  assert.equal(toolPhrase('browser_click'), 'Clicking through pages');
  assert.equal(toolPhrase('mcp__calendar__events'), 'Working in a connected app');
  assert.equal(toolPhrase('unrecognized'), undefined);
});

test('a running bot shows its tool phrase, or typing while text streams', () => {
  const bot = agent('a', 'Scout');
  const tool = run({ activity: 'tool', tools: [{ id: 't', runID: 'r1', name: 'web_search', status: 'running' } as never] });
  assert.equal(activityLine(activeActivities([bot], [tool], [], 'chat')), 'Digging around');
  const message = {
    id: 'm', threadID: 'chat', role: 'assistant', agentID: 'a', runID: 'r1', status: 'streaming', seq: 1,
    createdAt: '2026-01-01T00:00:00Z', parts: [{ kind: 'text', text: 'Hello' }], reactions: [],
  } as Message;
  assert.equal(activityLine(activeActivities([bot], [tool], [message], 'chat')), 'Typing…');
});

test('several active bots are named, and a model-authored status wins', () => {
  const first = agent('a', 'Ada');
  const second = agent('b', 'Bo');
  const third = agent('c', 'Cy');
  const working = run({ id: 'r1', agentID: 'a', activity: 'tool', tools: [{ id: 't', runID: 'r1', name: 'fetch', status: 'running' } as never] });
  const typing = run({ id: 'r2', agentID: 'b' });
  const waiting = run({ id: 'r3', agentID: 'c', status: 'waiting_peer', statusText: 'Searching for dates' });
  assert.equal(activityLine(activeActivities([first, second, third], [working, typing, waiting], [], 'chat')), 'Searching for dates…');
  assert.equal(activityLine(activeActivities([first, second], [working, typing], [], 'chat')), 'Ada and Bo are working…');
});
