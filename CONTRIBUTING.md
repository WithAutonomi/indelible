# Contributing to Indelible

Thanks for your interest in contributing to Indelible — an enterprise file-storage
front end for the [Autonomi Network](https://autonomi.com/). This document is a
set of guidelines, not rules, intended to make it easy for you to get involved.

Notice something amiss? Have an idea? Open an issue in this repository — bugs,
crashes, enhancement ideas, unclear docs, missing examples, anything you'd like
to see fixed or improved.

This project adheres to the
[Contributor Covenant](https://www.contributor-covenant.org/). By participating,
we sincerely hope that you honour this code.

## What we're working on

Development updates for the wider Autonomi project are published on the
[Autonomi Community Forum](https://forum.autonomi.community/). For general
documentation on building on Autonomi, see
[docs.autonomi.com](https://docs.autonomi.com/).

## Issues and feature requests

Bug reports should clearly lay out the problem, the platform(s) you experienced
it on, and steps to reproduce. Feature requests should clearly explain what the
proposed feature would include, resolve, or offer.

Labels we typically use:

- `bug` — defect in the product
- `feature` — new functionality
- `enhancement` — improvement to existing functionality or developer experience
- `blocked` — depends on a fix in a dependency
- `good first issue` — accessible for first-time contributors
- `help wanted` — lower priority for the maintainers but suitable for outside contributions

If you'd like to take on an open issue, please assign it to yourself first so
work isn't duplicated.

## Development

We follow a standard Git workflow: develop in branches, raise pull requests,
peer review, and merge only after CI passes. Direct commits to `master` are not
permitted.

### Toolchain

- **Go** — version pinned in `go.mod` (currently 1.25+). The backend is Go +
  [chi](https://github.com/go-chi/chi) + dual-dialect (SQLite/Postgres) via
  [goose](https://github.com/pressly/goose) migrations.
- **Node** — 22+ for the Vue 3 + PrimeVue frontend in `web/`.
- **Docker** — required for the postgres test matrix and the container build.

### Build

```bash
make frontend          # vue build into web/dist (embedded by the binary)
make backend           # go build → bin/indelible
make build             # both
```

### Tests

```bash
# Go tests, default SQLite dialect
go test ./...

# Same tests against Postgres (matches CI's postgres matrix)
INDELIBLE_TEST_DB_URL="postgres://postgres:ci@localhost:5432/postgres?sslmode=disable" \
  go test ./...

# Race detector
go test -race -count=1 -timeout 15m ./...

# Frontend
cd web && npm ci && npm run build && npm run test:unit
```

The `scripts/ci-local.sh` script runs the PR-gate subset (lint + vet + sqlite
tests + swag drift + frontend build) — mirror of what GitHub Actions runs on
every PR.

For the full heavy matrix (postgres tests, race detection, docker build/smoke,
Playwright E2E with a real OIDC IdP), use `scripts/ci-dev1.sh` to run it on a
Linux test host. See the comments in that file for setup expectations.

### Linting and formatting

- Go: `go vet ./...` and
  [`golangci-lint`](https://github.com/golangci/golangci-lint) — pinned to the
  version in `.github/workflows/ci.yml`. Run `golangci-lint run ./...` locally.
- Frontend: `cd web && npm run lint` if available; the build step is type-strict
  via `vue-tsc`.
- Swagger: handler annotations are checked for drift in CI. Regenerate with
  `swag init -g cmd/indelible/main.go -o docs/ --parseDependency` and commit the
  result.

### Commits

Clear, descriptive commit messages are appreciated; we don't require any
particular format but recent history follows [Conventional
Commits](https://www.conventionalcommits.org/) (`feat(...)`, `fix(...)`,
`chore(...)`, `ci(...)`, `test(...)`).

## Pull requests

If you're new to PRs, GitHub's
[first-contributions](https://github.com/firstcontributions/first-contributions)
walkthrough is a friendly starting point.

A few project conventions:

- PRs target `master`.
- Reference any related issue using a [GitHub
  keyword](https://help.github.com/articles/closing-issues-using-keywords) so
  the issue closes on merge.
- Aim for one issue/feature per PR. Smaller PRs (<= 200 lines changed) get
  reviewed faster. Split larger work into a sequence of focused PRs where
  possible.
- Rebase on the latest `master` rather than merging `master` into your branch.
  Expect to force-push during review.
- Push review-comment fixes as additional commits during review; squash them
  into the relevant commit once the reviewer is happy.
- Tests are expected for new behaviour or bug fixes. If a fix is environmental
  or non-testable, say so in the PR description.

## Releases

Releases are tagged by the maintainers. There is no automated semantic-version
release pipeline yet; tagged releases happen at a cadence appropriate to the
work landed. Versioning follows [Semantic
Versioning](https://semver.org/).

## License

By contributing to Indelible, you agree that your contributions will be
dual-licensed under [Apache 2.0](LICENSE-APACHE) and [MIT](LICENSE-MIT), at the
recipient's option, matching the project's existing licensing.

## Support

- **GitHub issues** — for bugs and feature requests in this repository
- **Discord** — <https://discord.gg/autonomi> for real-time discussion
- **Forum** — <https://forum.autonomi.community/> for longer-form discussion
- **Docs** — <https://docs.autonomi.com/> for general Autonomi documentation
