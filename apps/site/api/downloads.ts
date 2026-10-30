import releases from '../.generated/releases.json' with {type: 'json'};
import {createDownloads} from '../lib/server/downloads.ts';

export default createDownloads(releases);
