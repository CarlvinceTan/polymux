# Canvas

Shows bot activity, handoffs, repositories and agent reports on an editable board. Add cards and connections, edit their details, or drag live nodes into place. The plugin saves the workspace and each chat's board separately; board edits never start or change the underlying bot work. Requires a client and server with [web workspace support](../../docs/plugins.md#web-pages).

## Settings

- **Repositories**: paths to local checkouts, separated by commas. Each shows its branch, commit, uncommitted changes, and how far it is from its upstream.
- **GitHub**: reads pull requests and checks for those repositories through `gh`, using whatever account `gh` is signed in to. It only reads.

## Reporting work

Bots get `canvas__report_work` and `canvas__read_work`, and a skill saying when to use them. Anything else on the server can report through the API with the local token:

```sh
curl -H "Authorization: Bearer $(cat ~/.polymux/data/devices/local-token)" -H 'X-Polymux-Client: polymux-kit' \
  -d '{"nodes":[{"id":"nightly","title":"Nightly build","kind":"task","state":"working"}]}' \
  http://127.0.0.1:7337/plugins/canvas/api/report
```

A report adds or replaces nodes and wires by ID, and `remove` forgets them. Reports nobody has refreshed for a week drop off the graph.

Nodes can have a parent, connections and details. A node's `preview` names a PNG, JPEG or GIF on the server; the service serves a thumbnail. Video, audio and PDF have no preview yet.

## Testing

Run `scripts/test.sh plugins ./canvas/...` for unit tests. `python3 plugins/canvas/verify.py` installs the real package on a throwaway server with a scripted model and GitHub CLI. Add `--ui` to install and open Canvas in the throwaway Mac app instead.
