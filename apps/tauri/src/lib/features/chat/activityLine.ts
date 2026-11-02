import type { Agent, Message, Run } from '../../api/contract';

export interface ActiveActivity {
  agent: Agent;
  run: Run;
  typing: boolean;
  working: boolean;
  phrase?: string;
  status?: string;
}

const thinking = ['Thinking it through', 'Mulling it over', 'Putting it together'];
const working = ['Getting it done', 'Making progress', 'Working on it', 'Handling it', 'Chipping away'];

const tools: Record<string, string> = {
  websearch: 'Digging around', search: 'Digging around', placessearch: 'Digging around', weatherforecast: 'Digging around',
  httprequest: 'Doing some reading', webfetch: 'Doing some reading', fetch: 'Doing some reading',
  read: 'Reading the file', readfile: 'Reading the file', boxread: 'Reading the file', ls: 'Reading the file',
  listfiles: 'Reading the file', find: 'Reading the file', glob: 'Reading the file', grep: 'Reading the file', skill: 'Reading the file',
  write: 'Jotting it down', writefile: 'Jotting it down', edit: 'Jotting it down', editfile: 'Jotting it down', patch: 'Jotting it down', applypatch: 'Jotting it down',
  bash: 'Running a few commands', shell: 'Running a few commands', boxshell: 'Running a few commands', exec: 'Running a few commands', executecommand: 'Running a few commands',
  machineshell: 'Working on your computer', fetchconnectedactivity: 'Working on your computer',
  copytobox: 'Moving files over', copyfrombox: 'Moving files over', copybotfile: 'Moving files over', copyfile: 'Moving files over', movefile: 'Moving files over',
  await: 'Waiting on results', awaitshell: 'Waiting on results', wait: 'Waiting on results',
  generateimage: 'Sketching something up', generatevideo: 'Filming a clip', cloudagent: 'Writing some code',
  screenshot: 'Clicking around', computer: 'Clicking around', requestboxhelp: 'Clicking around',
  messageagent: 'Checking with another Bot', handoffagent: 'Checking with another Bot', spawntask: 'Checking with another Bot', sendtoagent: 'Checking with another Bot', updateagent: 'Checking with another Bot',
  createagent: 'Sending a message', sendtouser: 'Sending a message', reacttomessage: 'Sending a message', sendimessage: 'Sending a message',
  askuser: 'Needs your input', requestsecret: 'Needs your input', requestuserform: 'Needs your input',
  checksubscriptionusage: 'Checking your usage',
  manageintegrations: 'Working in a connected app', callmcptool: 'Working in a connected app', getmcptools: 'Working in a connected app',
  mcpauth: 'Working in a connected app', searchplugins: 'Working in a connected app', getplugin: 'Working in a connected app',
  installplugin: 'Working in a connected app', uninstallplugin: 'Working in a connected app', getmcpserverstatus: 'Working in a connected app',
  addmcpserver: 'Working in a connected app', uninstallmcpserver: 'Working in a connected app', authenticatemcpserver: 'Working in a connected app',
  restartmcpservers: 'Working in a connected app', setmcpinstructions: 'Working in a connected app', searchmcpservers: 'Working in a connected app',
  installmcpserver: 'Working in a connected app', enableteamserver: 'Working in a connected app',
};

function draw(values: string[], seed: string): string {
  let index = 0;
  for (const character of seed) index = ((index * 31) + character.charCodeAt(0)) & 0xffff;
  return values[index % values.length];
}

export function toolPhrase(name: string): string | undefined {
  const tail = name.split('.').pop() ?? name;
  const tool = tail.split('__').pop() ?? tail;
  if (tool.toLowerCase().startsWith('browser_')) return 'Clicking through pages';
  const key = tool.toLowerCase().replace(/[^a-z0-9]/g, '');
  if (key in tools) return tools[key];
  if (name.startsWith('mcp__')) return 'Working in a connected app';
  return undefined;
}

function latest(values: Run[]): Run | undefined {
  return [...values].sort((a, b) =>
    (Date.parse(b.createdAt) || 0) - (Date.parse(a.createdAt) || 0) || b.seq - a.seq)[0];
}

function streaming(run: Run, messages: Message[]): boolean {
  return messages.some((message) => message.runID === run.id && message.status === 'streaming'
    && message.parts.some((part) => !!part.text));
}

function activity(agent: Agent, run: Run, messages: Message[]): ActiveActivity | undefined {
  if (run.status === 'waiting_input') return undefined;
  if (!['running', 'resuming', 'waiting_peer'].includes(run.status)) return undefined;
  const live = streaming(run, messages);
  const tool = run.tools?.find((item) => item.id === run.toolID && item.status === 'running')
    ?? [...(run.tools ?? [])].reverse().find((item) => item.status === 'running');
  if (run.status === 'waiting_peer') {
    return { agent, run, typing: false, working: true, phrase: run.statusText ?? toolPhrase('message_agent'), status: run.statusText };
  }
  if (run.activity === 'tool') {
    const status = live ? undefined : run.statusText;
    return { agent, run, typing: live, working: true, phrase: live ? undefined : (status ?? toolPhrase(tool?.name ?? '') ?? draw(working, run.id)), status };
  }
  if (run.activity === 'rendering' || run.activity === 'handoff') {
    const status = live ? undefined : run.statusText;
    const phrase = live ? undefined : (status ?? (run.activity === 'handoff' ? toolPhrase('message_agent') : draw(working, run.id)));
    return { agent, run, typing: live, working: true, phrase, status };
  }
  const status = live ? undefined : run.statusText;
  return {
    agent, run, typing: run.trigger !== 'review', working: true,
    phrase: status ?? (live ? undefined : draw(thinking, run.id)), status,
  };
}

export function activeActivities(agents: Agent[], runs: Run[], messages: Message[], threadID: string): ActiveActivity[] {
  return agents.flatMap((agent) => {
    const run = latest(runs.filter((item) => item.conversationID === threadID && item.agentID === agent.id));
    const current = run && activity(agent, run, messages);
    return current ? [current] : [];
  });
}

export function activityLine(active: ActiveActivity[]): string {
  if (!active.length) return '';
  const status = active.find((item) => item.status)?.status;
  if (status) return active.length > 1 ? `${status}…` : status;
  if (active.length === 1) {
    const item = active[0];
    return item.phrase ?? (item.typing ? 'Typing…' : draw(working, item.agent.name));
  }
  const names = active.map((item) => item.agent.name);
  const listed = names.length <= 3 ? names : [...names.slice(0, 2), `${names.length - 2} others`];
  const joined = listed.length === 1 ? listed[0] : `${listed.slice(0, -1).join(', ')} and ${listed.at(-1)}`;
  const typing = active.every((item) => item.typing && !item.phrase);
  return `${joined} are ${typing ? 'typing…' : 'working…'}`;
}
