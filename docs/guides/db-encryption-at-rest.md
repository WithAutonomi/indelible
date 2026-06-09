# Database encryption at rest

*Requires host/operator access. This is a deployment-hardening guide — no
application setting turns it on; you encrypt the storage Indelible's database
lives on.*

## Why this matters

Indelible's application database is **plaintext on disk**. It holds more than
configuration:

- **Every private DataMap.** For a private upload, the DataMap lives *only* in
  this database (it is never published to the network). The DataMap is the only
  key to a private file — see the [private-DataMap landmine](backup-restore.md#why-this-matters-the-private-datamap-landmine).
  A copy of the DB file is effectively a master key to all private content.
- **Stored wallet private keys and OIDC client secrets** — encrypted with
  `INDELIBLE_WALLET_ENCRYPTION_KEY`, so those columns are *not* plaintext, but
  they're only as safe as that key (kept out-of-band).
- **User emails and login IPs** — PII subject to your compliance regime.

So a stolen disk, a lost backup tape, a snapshot copied to the wrong bucket, or
a decommissioned drive that wasn't wiped exposes private content and PII. Encrypting
the storage at rest closes that gap.

## What at-rest encryption does and does not protect

> **Threat model.** Disk encryption protects data on **media you no longer
> control**: a stolen/RMA'd drive, a leaked snapshot, a backup at rest. It does
> **not** protect a **running, compromised host** — while the service runs, the
> volume is mounted and the database is decrypted in use, so an attacker with
> root on the live box reads it regardless. Disk encryption is the at-rest layer;
> it is not a substitute for host hardening, least-privilege, and the
> application's own controls.

This is why the recommended pattern below is **storage-level** encryption
(cheap, transparent, covers the whole DB plus WAL, temp files, and logs) rather
than per-column application encryption — full coverage for the at-rest threat,
no key-management surface inside the app.

## Where the data lives

| Deployment | Database storage to encrypt |
|---|---|
| **SQLite** (single-host; default `INDELIBLE_DB_URL=sqlite:///var/lib/indelible/data.db`) | The data directory — the `.db` file **and its `-wal`/`-shm` sidecars**. In the Docker stack this is the `indelible-data` volume mounted at `/var/lib/indelible`. |
| **PostgreSQL** (canonical compose stack) | Postgres's data directory (`/var/lib/postgresql/data` → the `pg-data` volume), or the managed instance's storage. |

Encrypt the **underlying storage** for whichever you run. Don't forget the
**backups**: a backup of an encrypted DB written to unencrypted storage re-opens
the hole (see [Backups](#backups-inherit-this)).

## Recommended pattern

Pick the row that matches how you deploy. All are transparent to Indelible — no
config change, no app restart semantics.

### Self-hosted Linux (LUKS full-volume encryption)

Encrypt the filesystem that backs the data volume(s) with LUKS, then mount it
where the volume lives. Do this on a fresh/empty volume **before** first start
(or migrate data into it).

```bash
# 1. Create an encrypted container on a dedicated block device (e.g. /dev/sdb).
sudo cryptsetup luksFormat /dev/sdb
sudo cryptsetup open /dev/sdb indelible_crypt        # prompts for the passphrase
sudo mkfs.ext4 /dev/mapper/indelible_crypt

# 2. Mount it where Docker keeps named volumes (or bind-mount into the container).
sudo mkdir -p /mnt/indelible-data
sudo mount /dev/mapper/indelible_crypt /mnt/indelible-data

# 3. Point the Docker volume / DB path at the encrypted mount, then start.
#    e.g. a bind mount in docker-compose.yml:
#      volumes:
#        - /mnt/indelible-data:/var/lib/indelible      # (and the pg-data path for Postgres)
```

> **Unlock at boot.** A LUKS volume must be unlocked before the service starts.
> For unattended reboots, use a key file on separate protected storage or a TPM
> (`systemd-cryptenroll --tpm2-device=auto`) rather than typing a passphrase.
> Store the passphrase/recovery key in your secrets manager — **lose it and the
> volume (and every private DataMap on it) is unrecoverable.**

Verify it took:

```bash
sudo cryptsetup status indelible_crypt    # shows type: LUKS2, cipher, device
lsblk -o NAME,FSTYPE,MOUNTPOINT           # the mount sits on a `crypto_LUKS` parent
```

### Cloud VM (managed disk encryption)

Enable encryption on the disk backing the instance — it's transparent and on by
default for most providers:

- **AWS EBS** — create the volume with **encryption enabled** (KMS CMK of your
  choice); account-level "encrypt new EBS volumes by default" is recommended.
- **GCP Persistent Disk** — encrypted by default; supply a **CMEK** for control
  over the key.
- **Azure Managed Disks** — Server-Side Encryption is on by default; add a
  **customer-managed key** in a Key Vault for control.

Verify in the provider console / CLI that the disk reports *encrypted* with the
expected key.

### Managed PostgreSQL

Cloud-managed Postgres encrypts storage at rest, usually by default, with an
option to bring your own key:

- **AWS RDS / Aurora** — enable **storage encryption** at creation (KMS CMK).
  It cannot be toggled on in place; encrypt by restoring a snapshot into an
  encrypted instance.
- **GCP Cloud SQL** — encrypted at rest by default; **CMEK** optional.
- **Azure Database for PostgreSQL** — encrypted at rest; customer-managed key
  optional.

This covers the database, its WAL, and automated backups in one setting — the
simplest option when you're already on managed Postgres.

### Postgres TDE (self-managed)

If you self-manage Postgres and require encryption scoped to the database engine
rather than the volume, use a distribution with **Transparent Data Encryption**
(e.g. EDB Postgres Advanced Server). Community PostgreSQL has no built-in TDE, so
for self-managed community Postgres the **encrypted-volume** approach above is
the supported path.

## Backups inherit this

At-rest encryption on the live volume does **not** extend to backups you copy
elsewhere. Per [Backup & disaster recovery](backup-restore.md):

- Write SQLite file copies / `pg_dump` output to **encrypted storage**, or
  encrypt the dump itself (e.g. `age`, `gpg`) before it leaves the host.
- The **uploads NDJSON export is secret-grade** (every private DataMap in
  plaintext) — encrypt it at rest unconditionally.
- Keep `INDELIBLE_WALLET_ENCRYPTION_KEY` in your secrets manager, **separate from
  the DB backup**, so neither alone is sufficient.

## Key management

Whichever pattern you choose, the **encryption key is the new crown jewel**:

- Store volume passphrases / KMS key references in a secrets manager, not on the
  encrypted host itself.
- Rotate the storage/KMS key per your provider's procedure; this is independent
  of Indelible's application-secret rotation ([key rotation](key-rotation.md)).
- For where Indelible sources its *own* secrets (and the future option to source
  them from a KMS/Vault), see the secrets provider in
  [key rotation → Where secrets come from](key-rotation.md#where-secrets-come-from).

## Application-level (column) encryption — not built

Encrypting individual columns inside the database (SQLite via SQLCipher, or
Postgres `pgcrypto`) is **intentionally not implemented**. Volume/disk
encryption covers the at-rest threat end-to-end with no in-app key management,
and column encryption breaks indexing and querying on the protected columns.

If a specific compliance requirement ever demands column-level encryption for,
say, email or IP, it should be added so its key flows through Indelible's
**secrets provider** (`cfg.Secrets()`) — no new ad-hoc key path — and should
reuse the keyring's active-key-plus-history model so the column key can rotate
without a flag-day. Until such a requirement lands, the hardened-deployment
pattern above is the supported answer.
