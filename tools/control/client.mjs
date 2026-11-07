import { execFile } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const executable = fileURLToPath(new URL('../../scripts/control.sh', import.meta.url));

export class ControlError extends Error {
  constructor(response) {
    super(response.summary);
    this.name = 'ControlError';
    this.response = response;
  }
}

// This is the agent CLI transport, not a second input implementation or a Cua adapter.
export function observe(operation, request = {}) {
  return new Promise((resolve, reject) => {
    execFile(executable, [operation, JSON.stringify(request)], {
      timeout: 120_000, killSignal: 'SIGKILL', maxBuffer: 4 * 1024 * 1024,
    }, (error, stdout) => {
      let response;
      try { response = JSON.parse(stdout); } catch {
        reject(new ControlError({ ok: false, code: 'transport_failed', summary: 'Control returned no valid observation response.' }));
        return;
      }
      if (!response || typeof response.ok !== 'boolean' || typeof response.summary !== 'string') {
        reject(new ControlError({ ok: false, code: 'transport_failed', summary: 'Control returned an invalid response envelope; observation is unproven.' }));
        return;
      }
      if (error || response.ok !== true) {
        reject(new ControlError(response.ok === false ? response : {
          ok: false, code: 'transport_failed', summary: 'Control exited unsuccessfully; observation is unproven.',
        }));
        return;
      }
      resolve(response);
    });
  });
}

// One short-lived native process retains AX references for the whole interaction.
export async function connect(instance, { fixture = false } = {}) {
  const { spawn } = await import('node:child_process');
  const { createInterface } = await import('node:readline');
  const child = spawn(executable, ['session', JSON.stringify({ instance, fixture })], { stdio: ['pipe', 'pipe', 'pipe'] });
  let pending;
  let closed = false;
  let chain = Promise.resolve();
  const exited = new Promise(resolve => child.once('close', resolve));
  const failure = () => new ControlError({ ok: false, code: 'transport_failed', summary: 'Native session ended. An in-flight action may have taken effect; observe before retrying.' });
  const receive = () => new Promise((resolve, reject) => {
    const timer = setTimeout(() => { child.kill('SIGKILL'); reject(failure()); }, 120_000);
    pending = { resolve: value => { clearTimeout(timer); resolve(value); }, reject: error => { clearTimeout(timer); reject(error); } };
  });
  const ready = receive();
  createInterface({ input: child.stdout }).on('line', line => {
    const waiter = pending; pending = undefined;
    if (!waiter) { child.kill('SIGKILL'); return; }
    try {
      const response = JSON.parse(line);
      if (response.ok !== true) waiter.reject(new ControlError(response));
      else waiter.resolve(response);
    } catch { waiter.reject(failure()); child.kill('SIGKILL'); }
  });
  child.stderr.resume();
  child.stdin.on('error', () => { pending?.reject(failure()); pending = undefined; child.kill('SIGKILL'); });
  child.on('error', () => { closed = true; pending?.reject(failure()); pending = undefined; });
  child.on('exit', () => { closed = true; pending?.reject(failure()); pending = undefined; });
  const info = await ready;
  return {
    ...info,
    request(request) {
      const result = chain.then(() => {
        if (closed) throw failure();
        const line = JSON.stringify(request) + '\n';
        const response = receive();
        child.stdin.write(line);
        return response;
      });
      chain = result.catch(() => {});
      return result;
    },
    async close() {
      if (closed) return;
      try { await this.request({ op: 'close' }); }
      finally {
        child.stdin.end();
        if (!closed) child.kill('SIGTERM');
        await exited;
      }
    },
    async terminate() { child.kill('SIGKILL'); await exited; },
  };
}
