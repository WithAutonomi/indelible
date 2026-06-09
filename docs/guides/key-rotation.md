# Rotating the encryption key

`INDELIBLE_WALLET_ENCRYPTION_KEY` is the AES-256 key that encrypts two kinds of
secret at rest:

- **wallet private keys** (`wallets.encrypted_key`)
- **OIDC client secrets** (`oidc_providers.client_secret`)

Changing this env value **without re-encrypting those rows first** makes them
undecryptable — wallets can't sign payments and SSO logins fail. The
`rotate-keys` command re-encrypts every affected row from the old key to the new
one so rotation is safe.

> **Why a CLI, not an admin button:** rotation needs both the old and new key
> material in hand. Keeping it on the command line (run by an operator on the
> host) keeps live key material off the HTTP request path.

## How stored secrets are tagged

Each encrypted value is stored as a key-id-tagged envelope:

```
<keyid>:<hex(nonce‖ciphertext)>
```

`keyid` is a short, stable fingerprint of the key (`sha256(key)[:8]`). Values
written before this scheme have no `keyid:` prefix and are treated as encrypted
under the current key. The tag lets a partially-completed rotation stay
identifiable and recoverable — re-running `rotate-keys` is safe.

## Generating a new key

```bash
openssl rand -hex 32
```

## Rotating

Rotation is **offline** — run it while the service is stopped so nothing writes
new rows under the old key mid-rotation.

```bash
# 1. Stop the service.
docker compose stop indelible        # or: systemctl stop indelible

# 2. Re-encrypt all wallet keys + OIDC secrets from old -> new.
#    --config points at the same indelible.toml the server uses (for the DB URL).
indelible rotate-keys \
  --old "$CURRENT_WALLET_ENCRYPTION_KEY" \
  --new "$NEW_WALLET_ENCRYPTION_KEY" \
  --config /etc/indelible/indelible.toml

#    -> rotated 3 wallet(s) and 1 OIDC provider(s) from key 1a2b3c4d to key 9f8e7d6c

# 3. Update the env to the NEW key.
#    e.g. edit docker-compose.yml / the systemd unit / your secrets manager:
#    INDELIBLE_WALLET_ENCRYPTION_KEY=<new>

# 4. Start the service.
docker compose start indelible       # or: systemctl start indelible
```

After step 4, verify: an admin **Wallets** page that shows balances (a balance
read decrypts the key) and a test SSO login both confirm the rotation took.

## Safety notes

- **Run it in a transaction (it is):** `rotate-keys` re-encrypts all rows in a
  single DB transaction — it's all-or-nothing, so a failure mid-run rolls back.
- **Back up first** anyway (see [Backup & restore](backup-restore.md)).
- **Keep the old key** until you've verified the new one works — if you set the
  env to the new key but skipped step 2, set it back to the old key and re-run
  the rotation.
- If `--old` doesn't match the key the rows were actually encrypted under,
  `rotate-keys` fails loudly (per-row decrypt error) rather than corrupting data.
