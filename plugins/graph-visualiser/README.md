# Graph Visualiser

Shows work as a map: who's working, where, on what, and what's passing between people, bots, apps, devices and repositories. It's a view: nothing on it starts or changes work. [`docs/plugins.md`](../../docs/plugins.md#graph-visualiser) covers how it fits together.

## Settings

- **Repositories**: paths to local checkouts, separated by commas. Each shows its branch, commit, uncommitted changes, and how far it is from its upstream.
- **GitHub**: reads pull requests and checks for those repositories through `gh`, using whatever account `gh` is signed in to. It only reads.

## Reporting work

Bots get `graph_visualiser__report_work` and `graph_visualiser__read_work`, and a skill saying when to use them. Anything else on the server can report through the API with the local token:

```sh
curl -H "Authorization: Bearer $(cat ~/.polymux/data/devices/local-token)" -H 'X-Polymux-Client: polymux-kit' \
  -d '{"nodes":[{"id":"nightly","title":"Nightly build","kind":"task","state":"working"}]}' \
  http://127.0.0.1:7337/plugins/graph-visualiser/api/report
```

A report adds or replaces nodes and wires by ID, and `remove` forgets them. Reports nobody has refreshed for a week drop off the graph.

A node's `kind` decides how it's drawn, and a `pipeline` holding `step` nodes draws as steps along a road. A node's `preview` is the full path of a PNG, JPEG or GIF; the service serves a small copy, and reads nothing else. Video, audio and PDF have no preview yet.

## Testing

`scripts/test.sh plugins ./graph-visualiser/...`, and `python3 plugins/graph-visualiser/verify.py` runs the real package on a throwaway server with a scripted model. GitHub is scripted in the tests and off in `verify.py`, so neither reaches a real account.
