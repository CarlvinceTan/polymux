import {site} from '../site.ts';

export type Request = {query?: Record<string, string | string[] | undefined>};
export type Response = {
  statusCode: number;
  setHeader(name: string, value: string): unknown;
  end(body?: string): unknown;
};

export function sendJson(response: Response, status: number, body: unknown) {
  response.statusCode = status;
  response.setHeader('Content-Type', 'application/json; charset=utf-8');
  return response.end(JSON.stringify(body));
}

export function sendText(response: Response, status: number, type: string, body: string) {
  response.statusCode = status;
  response.setHeader('Content-Type', type);
  return response.end(body);
}

export function retire(response: Response, installer = false) {
  response.statusCode = 410;
  response.setHeader('Cache-Control', 'public, max-age=300');
  response.setHeader('Content-Type', installer ? 'text/x-shellscript; charset=utf-8' : 'application/json; charset=utf-8');
  const message = `This Polymux distribution is retired. Install the native app and server: ${site.origin}${site.installPath}`;
  return response.end(installer ? `#!/bin/sh\nprintf '%s\\n' '${message}' >&2\nexit 1\n` : JSON.stringify({error: message}));
}
