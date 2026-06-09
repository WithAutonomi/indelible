# Rotating keys and secrets

This service has two independent rotations:

- **the wallet encryption key** (`INDELIBLE_WALLET_ENCRYPTION_KEY`) — encrypts
  secrets at rest; rotate with the offline `rotate-keys` command (below).
- **the JWT signing secret** (`INDELIBLE_JWT_SECRET`) — signs session tokens;
  rotate with an overlap window so live sessions survive (see
  [Rotating the JWT secret](#rotating-the-jwt-secret)).

## Rotating the wallet encryption key

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

## Rotating the JWT secret

`INDELIBLE_JWT_SECRET` signs every session token (HS256). Changing it outright
**invalidates every live session at once** — everyone is logged out — because a
token is only valid if it verifies against the current secret.

To rotate without logging everyone out, keep the old secret as a **verify-only**
secret for one token lifetime. New tokens are always signed with the new
(primary) secret; the old secret only verifies tokens already issued under it,
until they expire.

- `INDELIBLE_JWT_SECRET` — the primary; **all new tokens are signed with this**.
- `INDELIBLE_JWT_SECRET_PREVIOUS` — comma-separated **verify-only** former
  secrets. A token validates if it verifies against the primary **or** any of
  these. Each must clear the same 32-character floor as the primary.

The **overlap window** is one token lifetime — the token TTL, which is the
login's "remember me" choice (`expiryHours`, max 24h). After that window every
token in circulation has been re-issued under the new primary, so the old secret
can be dropped.

### Procedure

```bash
# 0. Generate the new secret.
NEW=$(openssl rand -hex 32)

# 1. Make the current secret verify-only and the new one primary, then deploy.
#    (No downtime, no DB step — JWT secrets aren't stored anywhere.)
#    e.g. in docker-compose.yml / the systemd unit / your secrets manager:
INDELIBLE_JWT_SECRET="$NEW"
INDELIBLE_JWT_SECRET_PREVIOUS="<the secret that was just primary>"
#    (Already mid-rotation? Prepend, keep prior entries: "<prev1>,<prev2>".)

# 2. Restart the service so it picks up the new env.
docker compose up -d indelible        # or: systemctl restart indelible

# 3. Wait out one token lifetime (up to 24h — the max expiryHours).
#    Existing sessions keep working; new logins use the new secret.

# 4. Drop the old secret: remove INDELIBLE_JWT_SECRET_PREVIOUS (or remove just
#    the expired entry), then restart again. Rotation complete.
```

If a secret leaks and you must invalidate sessions immediately, skip the overlap:
set the new secret as primary with **no** previous list. Every existing token
stops validating at once (everyone re-authenticates) — which is the point.
