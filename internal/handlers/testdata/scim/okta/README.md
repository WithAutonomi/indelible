# Okta SCIM 2.0 fixtures — Tier 1.5

Captured via mitmproxy against a real Okta developer tenant on 2026-05-18 during
V2-273 Tier 2 rehearsal. Replayed in CI as Tier 1.5 catches regressions against
the actual Okta wire format without needing a live tenant.

## Capture context

- Okta SCIM Client: 1.0.0 (visible in each fixture User-Agent)
- App template: SCIM 2.0 Test App (Header Auth)
- indelible branch at capture: `nic/v2-273-scim-tier1`
- Capture proxy: mitmproxy 8.1.1 in reverse mode, cloudflared -> mitm -> indelible

## Fixtures

| File | What | Notable shape |
|---|---|---|
| `user_existence_check.json` | `GET /Users?filter=userName eq "..."` before any create | URL-encoded filter, count=100, startIndex=1 |
| `create_user.json` | `POST /Users` | Includes password, externalId, displayName, locale, emails[].type |
| `update_user.json` | `PUT /Users/{id}` (Okta uses full-PUT for profile edits, NOT PATCH) | Full resource replacement |
| `deactivate_user.json` | `PATCH /Users/{id}` with active=false | op=replace, value={active:false} — no explicit path |
| `reactivate_user.json` | `PATCH /Users/{id}` with active=true | Same shape as deactivate, opposite value |
| `group_metadata_patch.json` | `PATCH /Groups/{id}` no-op metadata confirm | Okta sends this BEFORE each membership op |
| `group_add_member.json` | `PATCH /Groups/{id}` adding a user | op=add, path=members, value=[{value, display}] |
| `group_remove_member.json` | `PATCH /Groups/{id}` removing a user | op=remove, path=members[value eq "X"] (filter-by-value) |

## Four findings worth knowing

1. Indelible silently ignores the `password` field in `POST /Users`. Okta ships
   an initial random password; indelible does not consume it. SCIM-provisioned
   users must authenticate via SSO. Safe behaviour.

2. Double-PATCH pattern on group membership: Okta sends a no-op `replace` of
   the group {id, displayName} before the real add/remove. Indelible must
   tolerate (and effectively no-op) the metadata PATCH.

3. Add and remove use different shapes:
   - add uses `path: "members"` + `value: [...]`
   - remove uses `path: "members[value eq \"X\"]"` (SCIM filter-by-value)
   Tier 1 must cover both.

4. The `op` field was lowercase in this capture. The capital-R `Replace` quirk
   seen historically did not appear in this Okta tenant. The Tier 1
   case-insensitive parsing remains valuable as a regression guard for older
   Okta orgs and future Azure AD captures.

## Replay test (follow-up)

`internal/handlers/scim_okta_fixtures_test.go` (not yet written) should:

1. Read each `*.json` in this dir
2. Issue the request method+path with `request.body` through the chi router
3. Assert the response matches `response.status` and `response.body` modulo
   `meta.created` and `meta.lastModified` which vary per run

## Data notes

Real captured payloads. Names, emails, and externalIds are from a throwaway
Okta dev tenant and a dummy user that has since been deactivated. No
production PII. The `password: "5qBqoy5N"` in `create_user.json` is an
Okta-generated random string for a deleted dev user; indelible discards it
on receipt.
