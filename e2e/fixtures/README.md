# Vault fixtures

`vault-kdbx.kdbx` is a synthetic KeePassXC-generated database used by `e2e/flows/vault_kdbx.js`.

- Database password: `e2e-kdbx-password`
- Entry: `KDBX E2E`
- Username: `kdbx-e2e-user`
- Password: `kdbx-e2e-secret`

Regenerate with KeePassXC:

```sh
keepassxc-cli db-create -p vault-kdbx.kdbx
keepassxc-cli add -u kdbx-e2e-user --url https://example.com -p vault-kdbx.kdbx 'KDBX E2E'
```
