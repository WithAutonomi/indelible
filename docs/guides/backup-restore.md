# Backup &amp; disaster recovery

*Requires admin permissions and host/operator access.*

This guide covers what to back up, how to restore, and the one thing that, if
lost, cannot be recovered: the **private DataMaps**.

## Why this matters: the private-DataMap landmine

Indelible stores files on the Autonomi network, which self-encrypts content and
addresses it by a **DataMap**. For a **private** upload, that DataMap is held
**only in Indelible's local database** — it is never published to the network.

> **The DataMap is the only key to a private file.** The encrypted chunks live on
> the network forever, but without the DataMap they cannot be located or
> decrypted. **Lose the database without a backup, and every private upload is
> gone permanently** — there is no recovery path, by anyone.

Public uploads are safer: their DataMap *address* is on the network, so a public
file can be retrieved from its address alone. But the catalog that maps your
filenames, owners, tags, and collections to those addresses still lives only in
the database.

So a complete backup is **layered**. Each layer protects something different.

> **At rest, too.** Because the database concentrates every private DataMap and
> all PII, encrypt the storage it lives on — and the backups themselves. See
> [Database encryption at rest](db-encryption-at-rest.md).

## What to back up

| Layer | Protects | How |
|-------|----------|-----|
| **1. Application database** | Everything: users, groups, wallets, settings, the upload catalog **and every private DataMap** | SQLite: copy the `.db` file (with its `-wal`/`-shm` sidecars). PostgreSQL: `pg_dump`. |
| **2. Uploads export (NDJSON)** | A **portable**, instance-independent copy of the upload catalog + DataMaps. Disaster recovery *and* anti-lock-in / migration | `GET /admin/uploads/export` (see below) |
| **3. Wallet encryption key** | The ability to *use* the wallets in the DB. `INDELIBLE_WALLET_ENCRYPTION_KEY` decrypts stored wallet private keys (and OIDC client secrets) — **without it, a restored DB's wallets are undecryptable** | Store the env value in your secrets manager, **separate from the DB backup**. To change it safely, see [Key rotation](key-rotation.md) |
| **4. Settings export** | Config: system settings, webhooks, OIDC providers (without secrets), groups | `GET /admin/settings/export` ([details](#settings)) |

Layers 1 and 2 overlap deliberately: the DB backup is your primary restore path;
the uploads export is a second, portable copy that survives a DB-format change,
a Postgres↔SQLite move, or migration to a fresh instance — and is the only one
of the two a third party can read without standing up Indelible.

> ⚠️ **The uploads export is secret-grade.** It contains every private DataMap in
> plaintext — effectively a master key to all private content. Treat the
> downloaded file exactly like a credential: encrypt it at rest, restrict access,
> and never commit it to a repo or ticket. Every export is recorded in the audit
> log (`uploads_exported`, severity `warn`).

## Exporting the upload catalog + DataMaps

The export is **NDJSON**: a single header line, then one JSON object per upload.
It streams, so it works regardless of catalog size.

```bash
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" \
  https://your-domain.com/api/v2/admin/uploads/export \
  -o indelible-uploads.ndjson
```

The first line is the header; subsequent lines are uploads:

```json
{"kind":"indelible-uploads-export","schema":1,"exported_at":"2026-06-08T10:00:00Z","count":2}
{"uuid":"…","owner_email":"alice@example.com","original_filename":"q3-report.pdf","visibility":"private","status":"completed","data_map":"…","tags":{"project":["apollo"]},"collections":["Reports"]}
{"uuid":"…","owner_email":"bob@example.com","original_filename":"logo.png","visibility":"public","status":"completed","datamap_address":"…"}
```

## Restoring

### Scenario A — full disaster recovery (rebuild a lost instance)

1. Stand up a fresh Indelible with the **same `INDELIBLE_WALLET_ENCRYPTION_KEY`**
   (layer 3). Without it, restored wallets cannot sign payments.
2. Restore the **application database** (layer 1): SQLite file copy, or
   `pg_restore`/`psql` for Postgres. This brings back users, wallets, settings,
   and the full upload catalog including private DataMaps in one step.
3. Verify: log in and **download one private file**. A successful private
   download proves the DataMaps survived.

The DB restore alone is sufficient for scenario A. The uploads export is the
fallback for when the DB itself is unusable (corruption, format change).

### Scenario B — migration / DB-unusable recovery (uploads export)

Use this to move uploads to a fresh instance, or when only the NDJSON export
survived.

1. Restore or recreate **users first** (via DB backup, SCIM re-sync, or
   re-invite) so ownership can be matched by email. This step is optional — see
   the owner-mapping note below.
2. Import the export:

   ```bash
   curl -fsS -X POST \
     -H "Authorization: Bearer $ADMIN_TOKEN" \
     -H "Content-Type: application/x-ndjson" \
     --data-binary @indelible-uploads.ndjson \
     https://your-domain.com/api/v2/admin/uploads/import
   ```

   > The `Content-Type: application/x-ndjson` header is required — it exempts the
   > (potentially large) restore body from the JSON request-size limit.

3. The response summarises the run:

   ```json
   {"imported":120,"skipped":0,"owner_fallback":2,"errors":[]}
   ```

**Import semantics**

- **Idempotent.** An upload whose `uuid` already exists on the target is
  **skipped**, not duplicated — re-running an import is safe.
- **No re-upload, no re-payment.** Records are recreated with their original
  `completed`/`already_stored` status, so the upload worker (which only processes
  `queued`/`processing`) never touches them and you are never charged again.
- **Owner mapping.** Each upload's owner is matched by email to an existing user;
  if no match is found, the upload is assigned to the admin running the import and
  counted in `owner_fallback`. So you can import before or after restoring users.
- **Tags** are restored. **Collection membership** is restored best-effort as
  flat (top-level) collections owned by the resolved owner; nested collection
  hierarchy is flattened.

## RPO / RTO guidance

- **RPO (how much data you can afford to lose).** Drive cadence off **upload
  volume**, not a fixed clock: every private upload between backups is
  unrecoverable if the DB is lost. For active instances, back up the DB at least
  daily and re-export uploads after any significant ingest. The export is cheap
  (metadata only) and safe to run often.
- **RTO (how fast you can be back up).** Scenario A is bounded by your DB restore
  time plus the one-private-download verification. Keep the wallet encryption key
  and a recent DB backup together-but-separate so neither is the long pole.
- **Test restores.** A backup you have never restored is a hypothesis. Periodically
  rehearse scenario A into a throwaway instance and confirm a private download.

## What is *not* covered

- The **file contents** themselves are not in any backup — they live on the
  Autonomi network and are durable there. Backups protect the *keys and catalog*
  that make them retrievable, not the bytes.
- **OIDC client secrets** are excluded from the settings export and must be
  re-entered after restore (see [settings export](#settings)).

<a id="settings"></a>
## Appendix: settings export/import

Admin → **Settings → Export** downloads `indelible-settings.json` (system
settings, webhook configs, OIDC providers **without** client secrets, group
definitions). Import accepts the structured format and the legacy flat format.
OIDC client secrets must be re-entered after import. All changes are recorded in
the config audit trail.
