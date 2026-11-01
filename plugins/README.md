# Polymux official plugins

Plugins maintained by the Polymux team. The app and server don't include them: each one is a package that's installed when someone asks for it.

| Plugin | What it does |
| --- | --- |
| [`hub`](hub) | Your messages and mail in one place, with bridges to chat networks. |
| [`tailscale`](tailscale) | Reaches your server from other devices over your tailnet. |
| [`permissions`](permissions) | Rules a chat's bots can't break: what they may read, write, run and use. |

## Layout

- `<name>/`: one folder per plugin, with its `plugin.json` and the Go program its `service` runs.
- `index.json`: the registry. CI adds each release to it on `dev`, and that copy is what polymux.com serves.
- `scripts/package.sh <plugin>`: builds `dist/plugins/<plugin>/<plugin>-<version>-<target>.tgz` and its checksum. A plugin that ships other programs gives a `prepare.sh` and packages them with `POLYMUX_PLUGIN_BINARIES`.
- `scripts/index.py`: merges a built package into an `index.json`.
- `permissions/verify.py`: runs the real package on a throwaway server with a scripted model, and checks that rules hold across file tools, commands, MCP calls, group chats and handed-off work. Run it after changing the gate.

This is one Go module, kept out of `go.work` so a plugin's dependencies can't change the server's build. Run its tests with `scripts/test.sh plugins`, and other `go` commands here with `GOWORK=off`.

## Release

Bump `version` in the plugin's `plugin.json`, then push a tag `<plugin>-v<version>` from `dev`, or run the **Release plugin** workflow with the plugin's name. It tests, builds `darwin-arm64`, `linux-amd64` and `linux-arm64`, publishes a release, and commits the updated `index.json` to `dev`.

## Try a package locally

```sh
plugins/scripts/package.sh permissions
python3 plugins/scripts/index.py permissions dist/plugins/permissions "file://$PWD/dist/plugins/permissions" --index dist/plugins/index.json
POLYMUX_PLUGIN_INDEX="file://$PWD/dist/plugins/index.json" scripts/run.sh
```

Then install the plugin from Settings → Plugins → Marketplace. The `tailscale` plugin changes the machine's real serve config, so don't install it on a test instance.

## Writing a plugin

A package is a `.tgz` with a `plugin.json` at its root and, for a plugin with a service, the executable it names. See [`docs/plugins.md`](../docs/plugins.md) for the manifest, the service contract and the capabilities.
