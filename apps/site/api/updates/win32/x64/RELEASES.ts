import {retire, type Request, type Response} from '../../../../lib/server/http.ts';

export default function handler(_request: Request, response: Response) { return retire(response); }
