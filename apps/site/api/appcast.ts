import releases from '../.generated/releases.json' with {type: 'json'};
import {createAppcast} from '../lib/server/appcast.ts';

export default createAppcast(releases);
