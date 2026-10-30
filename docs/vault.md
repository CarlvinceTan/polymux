# Vault

Manage entries in **Settings → Vault**, or through an agent’s private edit card in any chat. Browse names, websites, categories and search without an unlock step. Click an entry to edit its metadata or replace a value. **Saved values cannot be viewed, copied or revealed.** The editor shows “Saved” in an empty field; leaving it blank keeps the saved value, entering a value replaces it, and Clear removes it. Recovery codes follow the same replacement behavior and can be removed individually.

Add, edit and import stay inside the existing Settings view. There is no vault security modal, master password, Touch ID or Face ID flow. Encrypted KDBX imports still require that file’s password or key file.

## Reusable component boundary

The standalone Go module [`packages/vault`](../packages/vault/) owns encrypted storage, its tables, imports and the `Keys` interface. Callers supply a SQLite database, secure host-key storage and authorization. HTTP routes, agent tools and private cards are server adapters. See [Packages](packages.md#vault).

## Security boundary

Entries remain encrypted with a random data key held by macOS Keychain, or an owner-only key file on Linux. Existing vaults keep their encrypted entries and host key; after verifying that key, storage removes the retired password wrapper. A missing or invalid host key fails closed and never creates a replacement vault over existing entries. Keep that host key with the database when moving or recovering a server.

Paired-device authentication and encrypted transport protect management requests. Read and save responses contain metadata, empty field values and recovery-code identifiers/status only. The server merges replacements with the stored entry under its revision check. Password, biometric, reveal and TOTP-read endpoints are removed. Clients use ephemeral requests without cookies, disk caching or redirects, and responses are `Cache-Control: no-store`. Typed replacements are cleared when their editor closes; saved values never reach the client.

This removes ordinary readback and its unlock flow; it is not protection against a compromised host or an agent deliberately extracting secrets with unrestricted tools. Trusted server adapters still resolve values for origin-bound credential use. Names, websites and field names are discoverable metadata, so do not put secrets in titles. Private cards store references and acknowledgements, never decrypted fields, in chat history.

## Agent operations

- `vault_list`: discover item IDs and non-secret metadata. No field values or notes.
- `vault_manage`: add/edit through a private entry card; delete using the current revision; or configure the Chromium endpoint. No secret values are accepted as tool arguments.
- `vault_import`: supply a server file path for direct CSV/JSON import, or omit it for a private file picker. For encrypted KDBX, supply the path and optional key-file path, or choose the file privately; the user enters the KDBX password and key file only in that card. Returns counts only.
- `vault_request`: inject referenced fields into a JSON/form body, or use bearer/basic authentication. The destination must match the saved website's exact scheme, hostname, and port. HTTPS is required except on loopback. Redirects and environment proxies are disabled. Only HTTP status is returned; there is no authenticated-session/cookie export or generic response-body forwarding.
- `vault_fill`: fill visible inputs/textareas in a configured Chromium page at the saved origin. The origin is checked again in the page immediately before filling. Default bindings cover username/password or TOTP; explicit CSS bindings support cards and other fields. The operation does not submit the form. Agents should submit without inspecting the filled values or capturing visible secrets.
- `vault_write`: write one field's value to an owner-only file for a tool that needs the value itself, such as a key file, a `.netrc`, a `PGPASSFILE`, or a signer loading a key. The value never enters the tool result; the file is readable by the bot's commands, so delete it when the task is done. A recovery code is marked used. Prefer `vault_request` or `vault_fill` whenever they work.
- `vault_passkey`: temporarily install an imported ES256 software passkey in a Chromium virtual authenticator. The operation returns a ready ID; the agent triggers the site's sign-in and polls for completion. The authenticator is removed after use or two minutes, and successful signature counters are persisted. Keep existing CDP connections open during the ceremony.

Recovery codes are marked used and saved **before** dispatch or filling. Failures are not automatically retried; the destination may have accepted an operation even if its result is unknown. A user can inspect the account and explicitly mark an unused code available again. Generated TOTP values use SHA1/SHA256/SHA512, six/eight digits, and the configured period.

Choose **Settings → Vault → + → Import** to select a file privately, or use an agent's private import card. Settings imports require a paired device and never create chat attachments or accept server file paths.

Imports accept:

- UTF-8 CSV, including common `name/title`, `url/website`, `username`/`login_name`, `password`, `notes`, and TOTP columns used by Chrome, Bitwarden, 1Password, LastPass, and KeePass CSV exports; header variants differ.
- A JSON array of vault items (`title`, `kind`, `url`, `fields`, `codes`).
- An unencrypted Bitwarden JSON export, including logins, cards, secure notes, identities, custom fields, folders, and recovery-code fields.
- Encrypted KeePass KDBX 3.1 and 4.x files, with a master password, a key file, or both. Standard fields, TOTP seeds/URIs, recovery codes, common card fields, Wi-Fi fields, custom fields, and valid KeePassXC software-passkey fields are mapped. Group paths appear in notes; attachments, binaries, and history are not imported.

CSV column mappings can be supplied through `columns`, with canonical names as keys and source header names as values. CSV/JSON imports are bounded to 8 MiB; KDBX is bounded to 32 MiB with a 1 MiB key file. All formats are limited to 1000 entries, validated before writing, and committed atomically. Identical source records are skipped using keyed fingerprints; changed rows create new entries rather than overwriting existing ones. Source files remain untouched and CSV/JSON exports may still contain plaintext: remove them yourself when appropriate. Bitwarden's encrypted JSON and 1Password's `.1pux` archive are not decrypted here: export unencrypted JSON/CSV or use a supported CSV export.

**Do not attach credential exports as ordinary chat attachments.** Normal attachments can be indexed and included in model context before an import runs. Use a server file path or the private file picker, which uploads directly to the vault without chat attachment storage or indexing. The KDBX master password is sent only with the private import request; a key file is either uploaded there or read from the agent-supplied key path. Password and key-file contents are never tool arguments or chat text; only the optional key-file path is metadata. Existing chat attachments/history are not retroactively scrubbed. Entry saves and private imports commit their encrypted data and value-free continuation together.

For browser operations, configure an explicitly enabled **loopback Chromium debugging endpoint** using `vault_manage` with `operation: "browser"` and `endpoint`. Polymux does not start a browser, enable debugging, alter the user's browser profile, or expose a debugging endpoint. The reference browser must already be open at the saved origin. If several tabs match, supply an exact page target ID. Safari and Firefox are not supported by this adapter. Enabling debugging itself grants powerful access to the browser, so use an appropriate dedicated profile.

Passkey import accepts a relying-party ID, base64url credential ID/user handle, and an ES256 PKCS#8 private key. It does not register new passkeys, integrate with Apple Passwords/iCloud Keychain, or import non-exportable hardware-bound keys.

## Secret references

Saved secrets (from `request_secret`) and vault fields are black boxes an agent can use without seeing. It writes `{{secret:name}}` or `{{vault:id:field}}` wherever the value belongs, and Polymux fills it in only for the origin that secret or entry is bound to; elsewhere the reference stays literal. Saved secrets belong to the bot that requested them; vault entries are shared by every bot.

- `http_request`: references work anywhere in the URL, headers, body, or `basic` (`username:password`). The response is returned with every filled value redacted, including its URL-, JSON- and Basic-encoded forms.
- MCP connections: `headers` on an HTTP connection and `env` on a local one hold references, never values. Headers are filled when the connection opens, using the attaching bot's own secrets.
- Commands, scripts and local MCP servers run with `HTTPS_PROXY` pointing at a loopback proxy that only that bot's token can use, plus CA settings (`SSL_CERT_FILE`, `REQUESTS_CA_BUNDLE`, `CURL_CA_BUNDLE`, `GIT_SSL_CAINFO`, `NODE_EXTRA_CA_CERTS`) for a per-process CA that nothing else trusts. Traffic to an origin with a bound secret is decrypted there, references in the URL, headers (including Basic), and bodies up to 8 MiB are filled, and replies are redacted as they stream. All other traffic is tunnelled untouched. This needs a tool that honours the proxy variables and those CA settings: curl, Python, git, Node 24+ and Go CLIs such as `gh` do. Tools that sign requests with the key (for example AWS SigV4) cannot use a placeholder.

A program that has to read the value itself cannot use a reference or the proxy. For those, `write_secret` (saved secrets) or `vault_write` (vault fields) puts the value in an owner-only (0600) file at a path the agent chooses, atomically and without returning the value: an `ssh -i` key file, a `.netrc`, a `PGPASSFILE`, or a key a signer loads before signing (for example AWS SigV4). The agent then hands that path to the tool and deletes the file when the task is done. This trades the black box for compatibility on purpose: the bot's own commands can read that file, and it is the point at which a determined agent could put the value in its context, its output or a log. Use it only where references cannot work.

A secret can cover several origins (`origins`), which is what a tool spanning hosts needs: `gh` reads `https://api.github.com` and `https://github.com`, so one key bound to both is enough. A secret can also be temporary: `ttlMinutes` sets an expiry after which it is deleted on next use, and `ephemeral` keeps it in memory only, so it never reaches disk and disappears on restart. Both suit a key needed for one task; neither changes how the agent references it.

### SSH keys

`request_secret` with `kind: "ssh"` asks for a private key, giving the host it belongs to as `origin`. Commands then find it already loaded in `SSH_AUTH_SOCK`: `ssh`, `scp`, `rsync`, `git` over SSH and `gh` over SSH authenticate by asking the agent to sign, and the key itself is never readable by the command, the agent's output, or the model. A key saved with a passphrase cannot sign that way; it is still accepted and stays usable through `write_secret`, where the tool asks for it itself. The socket is per bot, owner-only (0600) and lives outside the data directory; the saved key itself can only be listed and signed with, never read.

The socket also offers any keys the host's own SSH agent already had, and `ssh-add` still writes to that host agent (or fails when there is none, as it did before), so saving a key never changes what a command could already do. A bot with no saved keys gets no socket at all, leaving the host agent exactly as it was.

This is a signing oracle rather than an origin binding: a command that can reach the socket can sign for any host, so the host is a label, not an enforced boundary. Rotate or forget the key (`forget_secret`) when it should stop working.

## Management API

The wire types and routes are in `contracts/polymux.schema.json`. All routes require device authentication and HTTPS or loopback transport. Browser-origin requests are refused. There is no separate unlock token. Item responses contain metadata and blank replacement fields only.

| Route | Operation |
| --- | --- |
| `GET /vault` | Creation status and unattended-key availability |
| `GET /vault/items` | List value-free summaries |
| `GET /vault/items/{id}` | Read metadata and blank replacement fields |
| `PUT /vault/items/{id}` | Create or replace fields; current revision required for edits |
| `DELETE /vault/items/{id}?revision=N` | Delete only the specified current revision |
| `GET /vault/browser`, `PUT /vault/browser` | Manage the browser endpoint |
| `POST /vault/import` | Import a private CSV/JSON or multipart KDBX body from Settings; no chat card or server paths |
| `POST /vault/cards/{id}/save` | Save a private edit and its value-free acknowledgement atomically |
| `POST /vault/cards/{id}/import` | Import a private raw CSV/JSON or multipart KDBX body; never a chat attachment |

Blank field values preserve existing content. Nonempty values replace it, and `removed` names explicitly cleared fields. Recovery-code IDs and usage flags can be edited without returning their values. Revision checks prevent stale writes.

## Storage format

The standalone Go server owns two tables in its existing SQLite database:

- `vault_meta`: one row (`id=1`) with encrypted key `proof` and `browser` configuration.
- `vault_items`: item ID, revision, and encrypted item JSON, including its title, website, fields, and recovery-code usage.

Version 1 uses a random 32-byte data key and AES-256-GCM. Each encrypted blob has a fresh nonce and associated data binding it to its purpose and item ID/revision. The ciphertext format is preserved for existing entries. The exact encoding is defined in `packages/vault/store.go`.

On macOS the unattended key is stored as hex under Keychain service `com.polymux.vault`, with an account equal to the SHA-256 hex digest of the absolute server vault-directory path. Keychain writes do not put the key in process arguments. On Linux the raw key is in `<data-dir>/vault/key`, mode `0600`, under a `0700` directory. Key material must not be included in an unprotected export alongside the database.

The SQLite representation can be exported without vendor tooling. A usable restore needs both the database and its protected host key; the retired vault password cannot recreate a missing key. Protect any export containing both. KDBX and vendor JSON can be imported; there is no KDBX export.

## Verification

Tests use throwaway databases, synthetic credentials, and disposable Chromium profiles—not the user's vault or browser profile.

```sh
cd apps/server
go test -race -tags sqlite_fts5 ./internal/server -run 'Test(Vault|Credential)'
(cd ../../packages/vault && GOWORK=off go test -race ./... && GOWORK=off go test -tags integration -run '^TestChromium' -v)
```

From the repository root:

```sh
swift test --filter VaultTests
scripts/check.sh all
scripts/run-e2e.sh e2e/flows/vault.js e2e/flows/vault_kdbx.js e2e/flows/settings.js e2e/flows/vault_browser.js e2e/flows/vault_values.js
scripts/run-e2e.sh --driver control e2e/flows/vault_passkey.js
```

The flows cover browsing/search, direct entry and replacement, clearing values, recovery-code entry, private chat cards, passkeys, and encrypted KDBX import. Server and client tests verify that no read or save response returns saved values, metadata edits preserve hidden content, stale edits fail, cancelled editors stay closed, imports are atomic, old encrypted entries migrate without re-encryption, and credential-use operations retain their existing boundaries. Optional Chromium tests exercise real filling and WebAuthn use.
