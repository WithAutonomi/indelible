# Real E2E tests

This directory holds **full user-journey** end-to-end tests — uploads that
go all the way through antd to the Autonomi network, admin-provisions-quota
flows that block real uploads, full SCIM provisioning round-trips, etc.

## What goes here vs `smoke/`

| `smoke/` | `e2e/` |
|---|---|
| Page-load checks, register form, admin-token reaches /admin/settings | Real uploads, real downloads, byte-level content verification |
| Runs on every PR (cheap, ~60s of actual test work) | Runs on **dev1 only**, never in GitHub Actions CI |
| No external dependencies (sqlite in-memory) | Requires running antd + funded wallet + EVM devnet |
| Use for "did the binary boot and serve a page?" | Use for "does the whole product actually work?" |

Trigger from the project root:

```bash
make ci-dev1 ARGS="--only e2e"
```

## Preconditions

Tests self-skip when these aren't met (so partial environments still give
useful signal):

1. `/health` reports `antd: true` — antd is running and reaches peers.
2. `/api/v2/system/wallet-status` reports `has_default_wallet: true`, OR
   `TEST_WALLET_KEY` can pay on whatever EVM network indelible is pointed
   at. Devnet/local is the expectation; real-network upload tests are
   out of scope for this suite.

## Why not in CI

These tests need:
- The `antd` binary on PATH (or `INDELIBLE_ANTD_URL` pointing at one).
- A funded wallet on a real or local EVM network.
- Minutes-not-seconds runtime per test.

GitHub Actions doesn't have any of that, and standing it up would burn the
budget we're trying to preserve. dev1 is the right home — it already has
antd + the devnet config the operator uses for manual testing.

## Adding a test

1. Drop a new `*.spec.ts` under `tests/`. Reuse `helpers/auth.ts` for the
   shared admin token (created by `global-setup.ts`).
2. Use `test.beforeAll` to skip when the test's specific preconditions
   aren't met. Don't fail-by-default — a missing antd is a setup gap, not
   a regression.
3. Keep timeouts realistic. Default test timeout is 120s; bump higher on
   the test itself with `test.setTimeout(...)` if your flow needs it.
4. Tests run with `retries: 0`. State leaks between retries (real wallet,
   real network) usually mask the original failure with cascading errors.
