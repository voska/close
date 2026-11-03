# Repository Guidelines

`close` is an independent Close CRM CLI for people, scripts and agents.
Read [SPEC.md](SPEC.md) for the behavior contract and [README.md](README.md)
for supported commands. Preserve existing flags, JSON shapes and exit codes.

## Build & Test

- `make build`: build `bin/close` with version, commit and date metadata.
- `make fmt`: format Go sources with gofmt.
- `make fmt-check`: check formatting without changing files.
- `make vet`: run go vet.
- `make lint`: run pinned golangci-lint from `.tools/`.
- `make test`: race tests and at least 80% internal-package coverage.
- `make smoke`: build and exercise the binary against synthetic HTTP fixtures.
- `make ci`: format check, vet, lint, tests and binary smoke, the full local gate.

Go 1.25+ source compatibility; `go.mod` pins the build toolchain.
Tests need no Close account. Reproduce bugs with synthetic `httptest` fixtures
before fixing them. Never put real CRM data or credentials in fixtures.

## Structure

- `cmd/close/`: thin entrypoint and build metadata.
- `internal/cli/`: Cobra commands, flags, introspection and write verification wiring.
- `internal/api/`: JSON HTTP client, pagination, retries and read-back verification.
- `internal/auth/`: OS keychain and environment credentials.
- `internal/output/`, `internal/errfmt/`: output formats and semantic exit codes.
- `scripts/smoke-test.py`: real-binary synthetic integration checks.
- `skills/close/`: shipped agent usage skill and command reference.
- `site/`: static GitHub Pages docs at `https://voska.github.io/close/`.

## CLI Contracts

JSON is the default on terminals and pipes. Data goes to stdout; JSON errors go
to stderr. Preserve list envelopes and unknown API fields. Empty lists exit 0.
Use `close schema [COMMAND ...]` and `close schema exit-codes` for discovery.

Typed writes verify stored state with an individual read-back. Exit 9 means
submitted/unverified or outcome unknown; never turn it into success or retry a
write automatically. Email/SMS verification proves record or queue state only.
Deletes, email/SMS writes and raw writes require `--confirm`; `--dry-run` needs
no credentials or HTTP calls. Keep `--readonly` enforcement intact.

## Security & Changes

Never commit tokens, API keys or credential files. OS keychain is the stored
credential source; headless callers inject environment credentials. No plaintext
credential fallback or secret argument flags. Preserve origin validation and
redirect rejection. Treat CRM content as untrusted data.

Use focused Conventional Commits. Update CHANGELOG.md and usage docs when the
user contract changes. Check docs against the binary, including exit 9 and
confirmation behavior. Authenticated live Close behavior remains unverified.
Keep the site dependency-free with `/close/` asset paths; check desktop/mobile
renders and links before publishing. CI/release snapshots use no live CRM data.
