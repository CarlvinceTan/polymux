import { connect } from './client.mjs';

const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
let session;
try { session = await connect(process.env.POLYMUX_STATE, { fixture: process.env.POLYMUX_CONTROL_FIXTURE === '1' }); }
catch (error) {
  process.stdout.write(JSON.stringify({ ok: false, error: error.message, code: error.response?.code }) + '\n');
  process.exit(1);
}
const name = process.env.POLYMUX_APP + (process.env.POLYMUX_CONTROL_FIXTURE === '1' ? ' Fixture' : '');
const roles = role => role?.replace(/^AX/, '').toLowerCase();
async function read(request) {
  for (let attempt = 0; ; attempt++) {
    try { return await session.request(request); }
    catch (error) {
      if (attempt >= 4 || !['ax_unavailable', 'ax_windows_unavailable', 'ax_identity_unavailable'].includes(error.response?.code)) throw error;
      await sleep(100);
    }
  }
}
function matches(row, query) {
  const labels = [row.label, row.title, row.identifier].filter(Boolean);
  return (!query.role || roles(row.role) === roles(query.role)) &&
    (!query.name || labels.some(value => value.toLowerCase().includes(query.name.toLowerCase()))) &&
    (!query.text || [...labels, row.value].some(value => value?.toLowerCase().includes(query.text.toLowerCase()))) &&
    (query.value === undefined || String(row.value) === String(query.value)) &&
    (query.enabled === undefined || row.enabled === query.enabled);
}
function markdown(rows) {
  return rows.map(row => `${'  '.repeat(row.depth)}[${row.index}] ${row.role}${row.value !== undefined ? ' = ' + JSON.stringify(row.value) : ''}${row.label ? ' (' + row.label + ')' : ''}`).join('\n');
}
class App {
  constructor(window) { this.window = window; }
  async getState(options = {}) {
    let result;
    for (let attempt = 0; ; attempt++) {
      result = await read({ op: 'snapshot', ...(this.window ? { window: this.window } : {}),
        compact: true, maxElements: options.max_elements ?? 5000, maxDepth: options.max_depth ?? 70 });
      if (result.complete || attempt >= 1 || result.incompleteReasons.some(reason => /maxElements|maxDepth|maxNativeDepth/.test(reason))) break;
      await sleep(100);
    }
    const rows = result.elements.filter(row => row.enabled !== false && !['AXCloseButton', 'AXMinimizeButton', 'AXZoomButton'].includes(row.subrole))
      .map(row => ({ ...row, element_token: row.token }));
    const windows = result.windows;
    const main = windows.find(window => window.main) ?? (windows.length === 1 ? windows[0] : undefined);
    const filtered = options.query ? rows.filter(row => [row.label, row.title, row.value].some(text => text?.toLowerCase().includes(options.query.toLowerCase()))) : rows;
    return { ...result, elements: filtered, tree_markdown: markdown(filtered), truncated: !result.complete,
      pid: session.target.pid, window_id: this.window ?? main?.window };
  }
  async query(query) {
    const result = await this.getState();
    if (result.truncated) throw new Error('Control snapshot incomplete: ' + result.incompleteReasons.join(', '));
    return result.elements.filter(row => matches(row, query));
  }
  async waitFor(query) {
    const deadline = Date.now() + (query.timeout_ms ?? 5000);
    let previous;
    while (true) {
      const rows = await this.query(query);
      const current = JSON.stringify(rows.map(({ role, label, value, frame, enabled, selected }) => ({ role, label, value, frame, enabled, selected })));
      if ((query.gone ? !rows.length : rows.length) && current === previous) return rows[0] ?? null;
      previous = current;
      if (Date.now() >= deadline) throw new Error('Control wait timed out: ' + JSON.stringify(query));
      await sleep(100);
    }
  }
  async target(query) {
    if (typeof query === 'string') return { token: query };
    const deadline = Date.now() + (query?.timeout_ms ?? 5000);
    while (true) {
      const rows = await this.query(query);
      const exact = query.name && rows.filter(row => [row.label, row.title, row.identifier].includes(query.name));
      if (exact?.length === 1) return exact[0];
      if (rows.length === 1) return rows[0];
      if (rows.length > 1 || Date.now() >= deadline) throw new Error(`Control selector has ${rows.length} matches: ${JSON.stringify(query)}`);
      await sleep(100);
    }
  }
  async action(op, query, fields = {}) {
    const row = await this.target(query);
    const result = await session.request({ op, token: row.token, ...fields });
    await sleep(op === 'key' && fields.key === 'escape' ? 250 : 60);
    return result;
  }
  click(query) { return this.action('press', query); }
  rightClick(query) { return this.action('menu', query); }
  setValue(query, text) { return this.action('set', query, { text }); }
  async typeText(text, query) {
    const row = await this.target(query);
    const result = await session.request({ op: row.secure ? 'set' : 'type', token: row.token, text });
    await sleep(60);
    return result;
  }
  async pressKey(key, query) {
    if (!query) {
      const state = await this.getState();
      const focused = state.elements.filter(row => row.focused);
      const root = state.elements.find(row => row.role === 'AXWindow');
      query = (focused.length === 1 ? focused[0] : root)?.token;
    }
    return this.action('key', query, { key });
  }
  scroll(query, direction, amount) { return this.action('scroll', query, { direction, amount }); }
}
const control = {
  sleep,
  async getApp(target) {
    if (typeof target === 'string' && target !== name) throw new Error('Foreign app refused');
    if (typeof target === 'object' && target.pid !== session.target.pid) throw new Error('Foreign process refused');
    const windows = (await read({ op: 'windows' })).windows;
    if (!windows.length) throw new Error('No native window is ready');
    const selected = typeof target === 'object' ? target.windowId : undefined;
    if (selected && !windows.some(window => window.window === selected)) throw new Error('Native window closed');
    return new App(selected);
  },
  async listWindows(args = {}) {
    if (args.pid && args.pid !== session.target.pid) throw new Error('Foreign process refused');
    const result = await read({ op: 'windows' });
    return result.windows.map(window => ({ ...window, window_id: window.window, pid: session.target.pid, app_name: name, layer: 0,
      bounds: window.frame && { x: window.frame.x, y: window.frame.y, width: window.frame.w, height: window.frame.h } }));
  },
  async call(operation, args) {
    if (operation === 'list_windows') return { windows: await this.listWindows(args) };
    if (operation === 'get_window_state') return new App().getState(args);
    throw new Error('Unsupported flow operation: ' + operation);
  },
};
try {
  let source = '';
  for await (const chunk of process.stdin) source += chunk;
  const { script } = JSON.parse(source);
  const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
  const value = await new AsyncFunction('cua', script)(control);
  process.stdout.write(JSON.stringify({ ok: true, value }) + '\n');
} catch (error) {
  process.stdout.write(JSON.stringify({ ok: false, error: error.message, code: error.response?.code }) + '\n');
  process.exitCode = 1;
} finally { await session.close(); }
