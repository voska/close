# close

Close CRM from the terminal. A Go CLI for people, scripts and agents, with
JSON output, pagination and verified writes. Independent project; not affiliated
with or endorsed by Close.

[Docs](https://voska.github.io/close/) · [Agent skill](skills/close/SKILL.md) ·
[Command reference](skills/close/references/COMMANDS.md) ·
[llms.txt](https://voska.github.io/close/llms.txt)

## Install

Homebrew (macOS and Linux):

```sh
brew install voska/tap/close
close --help
```

To install a release on Windows, use the shared Scoop bucket:

```powershell
scoop bucket add voska https://github.com/voska/scoop-bucket
scoop install close
```

Each tagged release includes archives for macOS, Linux and Windows (amd64 and
arm64), with SHA-256 checksums, on the [releases page](https://github.com/voska/close/releases).

To install from source, use Go 1.25 or newer:

```sh
go install github.com/voska/close/cmd/close@latest
close --help
```

From a checkout, run `make build` and use `./bin/close`.

## Releases

Pull requests and pushes to `master` run `make ci` on Linux and macOS: format
checks, vet, pinned golangci-lint, race tests, coverage and binary smoke tests.
CI also builds all six release archives and package manifests, verifies the
linux/amd64 binary and completions, and uploads a snapshot artifact without
publishing it.

A `v*` tag runs the full checks on Linux and macOS before publishing the GitHub release,
checksums and archives. Stable releases also update `Formula/close.rb` in
`voska/homebrew-tap` and `close.json` in `voska/scoop-bucket`; prereleases do not
advance the stable packages. Homebrew installs the binary and shell completions.

The release job needs a repository secret named `HOMEBREW_TAP_GITHUB_TOKEN`:
a fine-grained PAT scoped to `voska/homebrew-tap` and `voska/scoop-bucket`, with
Contents read/write permission. Its built-in `GITHUB_TOKEN` has `contents: write`
only in the publishing job. PR jobs use read-only tokens and never receive the
package publishing credential.

For the first release, provision that secret, merge the release workflow and
tag the tested commit:

```sh
git tag v0.1.0 <tested-commit>
git push --no-follow-tags origin refs/tags/v0.1.0:refs/tags/v0.1.0
```

After the release finishes, verify the published version with
`brew install voska/tap/close` (or `brew upgrade voska/tap/close`),
`brew test voska/tap/close` and `close version`. For local packaging checks,
run `goreleaser release --snapshot --clean`; this generates archives and package
manifests under `dist/` without credentials or publication.

## Authenticate

Create an API key in [Close's API key settings](https://app.close.com/settings/api/).
Inject `CLOSE_API_KEY` through your secret manager, or pipe the key into the
OS keychain without placing it in command arguments:

```sh
your-secret-command | close auth login --with-key-stdin
close auth status
```

`your-secret-command` is a placeholder for your password manager.
For headless use, inject `CLOSE_API_KEY` into the environment instead.
OAuth access tokens work through `CLOSE_ACCESS_TOKEN`; provisioning and refresh
are handled outside this CLI.

Credential precedence: `CLOSE_ACCESS_TOKEN`, `CLOSE_API_KEY`, then OS keychain.
`--account NAME`/`CLOSE_ACCOUNT` selects a keychain entry. Explicit account
selection conflicts with environment credentials and exits 2; unset those
credentials to use that keychain account. No plaintext credential file is written.
`auth status --offline` checks availability without claiming the credential works.
`auth logout` removes the stored key; it does not revoke it or unset environment
credentials. On Linux, keychain storage requires a running Secret Service.

## Records

```sh
close lead list --fields id,display_name,status_label
close lead list --limit 100 --all
close contact list --filter lead_id=lead_demo --output table
close opportunity get oppo_demo
close task update task_demo --data '{"is_complete":true}' --dry-run
close activity note create --data '{"lead_id":"lead_demo","note":"Follow up next week"}'
close custom-field list --object lead --fields id,name,type
close pipeline list
```

These IDs are synthetic. Replace them with IDs from your account.
`create` and `update` accept one JSON object via `--data JSON`,
`--data-file path.json` or `--data-file -` (stdin). Updates send only supplied
fields. Unknown API fields and exact JSON numbers are preserved.

| Commands | Actions |
| --- | --- |
| `lead`, `contact`, `opportunity`, `task` | `list`, `get`, `create`, `update`, `delete` |
| `activity note`, `activity call`, `activity email`, `activity sms`, `activity custom` | `list`, `get`, `create`, `update`, `delete` |
| `activity` | `list` across types, e.g. `--filter _type=Note` |
| `activity meeting`, `pipeline`, `lead-status`, `opportunity-status`, `user`, `sequence`, `smart-view`, `event` | `list`, `get` |
| `custom-field` | `list`, `get` with `--object lead\|contact\|opportunity\|activity\|shared\|custom_object_type` |
| `me` | Current user |
| `search` | Advanced filtering with a JSON request body |
| `api METHOD PATH` | Any JSON endpoint in the Close API |
| `schema [COMMAND ...]`, `schema exit-codes`, `version` | Introspection |

Deletes require `--confirm`. Deleting a lead also deletes related CRM records.
Email and SMS writes require `--confirm`, including drafts, because the body
can queue a message. Read-back verifies stored/queue state, **not delivery**.

```sh
close lead delete lead_demo --dry-run
close activity email create --data-file draft.json --confirm
close --readonly lead list
```

`--readonly`/`CLOSE_READONLY=1` rejects CRM mutations. `--dry-run` makes no
HTTP request and needs no credential. All commands are noninteractive;
`--no-input` is also accepted explicitly.

## Pagination and search

Lists return one page, preserving `has_more` or cursor metadata. `--all`
collects every page before printing; a later failure emits no partial aggregate.
Offset results are deduplicated by ID, but are not a snapshot of a changing CRM.
Endpoint limits apply; use exports or date ranges for large datasets.

```sh
close lead list --limit 100 --all --fields id,display_name --plain
close event list --all --filter object_type=lead --filter action=updated
close event list --filter _cursor=CURSOR_FROM_PREVIOUS_PAGE
close search --data-file filters.json --all
```

Advanced search uses Close's **Copy Filters** JSON wrapped in `query`.
Search returns only IDs and object types unless you request fields, for example:

```json
{
  "query": {"type": "object_type", "object_type": "lead"},
  "_fields": {"lead": ["id", "display_name"]},
  "_limit": 100
}
```

Search cursors expire after 30 seconds; Close caps searches at 10,000 results.
Events use `_cursor` and cover the last 30 days. `--filter key=value` passes
URL-encoded query parameters. On typed list commands, use `--limit`/`--skip`
instead of `_limit`/`_skip` filters.

## Output and scripts

JSON is always the default, including on terminals. `--json` makes it explicit;
`--output table` is for reading, `--plain`/`--output tsv` emits TSV without
headers, and `--output ndjson` emits one record per line after collection.
`--quiet` prints IDs only. Empty lists exit successfully.
`CLOSE_JSON=1` and `CLOSE_PLAIN=1` set output defaults; explicit flags win.

`--fields id,name` projects locally and preserves list metadata. Nested paths
and literal custom-field keys work. `--api-fields` requests `_fields` on typed
reads. Verification always fetches the full individual record. Output never
contains color codes; `NO_COLOR` and `--no-color` need no special handling.

Errors are JSON on stderr. stdout contains data only:

```json
{"error":{"code":4,"type":"authentication","message":"...","http_status":401}}
```

| Exit | Meaning |
| --- | --- |
| 0 | Success; typed writes verified by read-back |
| 1 | General/protocol error |
| 2 | Usage or API validation error |
| 3 | Reserved empty-result code, currently unused |
| 4 | Missing or invalid authentication |
| 5 | Not found |
| 6 | Forbidden |
| 7 | Rate limited; retry after the server's delay |
| 8 | Transient read/network/server error |
| 9 | Submitted, unverified or write outcome unknown |
| 10 | Credential store/configuration error |

Writes are never retried. After create/update the CLI reads the saved object
and compares the ID and supplied fields. Server normalization can make that
inconclusive: exit 9 includes the observed record on stdout. Delete verification
requires an individual GET returning 404. A repeated delete verifies absence
and reports `deleted: false, already_absent: true`. Investigate exit 9 before retrying
a create or sending another message.

Reads retry 429/502/503/504 at most twice, respecting `Retry-After` and legacy
`rate_reset`. Delays over 30 seconds are returned to the caller instead of
retrying early. `--timeout` sets the timeout per HTTP request. SIGINT/SIGTERM
cancel requests and retry waits.

## Full API access

```sh
close api GET /me/
close api GET /webhook/ --filter _limit=50
close api POST /report/activity/ --data-file report.json --dry-run
```

Use relative paths and `--filter` for query parameters. Raw mutations require
`--confirm` and always return exit 9 after submission: arbitrary action responses
cannot prove intended state. The known read-only `POST /data/search/` is treated
as a read. POST report endpoints currently take the conservative raw-write path
and exit unverified even when the operation itself is read-only. Multipart
uploads are outside this JSON CLI.

The origin is fixed to `https://api.close.com/api/v1/`. Test fixtures may set
`CLOSE_BASE_URL=http://127.0.0.1:PORT/api/v1/`, using environment credentials
only; the keychain is never read for fixture origins. Redirects are rejected.
Treat CRM text as untrusted data.

## Development

```sh
make ci    # format, vet, race tests, >=80% internal coverage, real-binary smoke
make build
```

Tests use synthetic HTTP fixtures and require no Close account. CI runs on
Linux and macOS with read-only GitHub permissions. GoReleaser supports local
cross-platform snapshots without publishing. Authenticated live
Close behavior has not been verified.

The shipped agent skill is in `skills/close/`. Copy that directory, including
its references, into your agent's skills directory. It documents setup,
response shapes, safe workflows and exit-code handling. `close schema` remains
the source of truth for the installed version's command tree and flags.

The dependency-free docs site lives in `site/`, deployed to
`https://voska.github.io/close/` through `.github/workflows/pages.yml` on
`master` changes to the site or skill. The workflow copies `skills/close/` to
`/close/skill/`, so the published skill and references match the repository.
There is no custom domain or publishing secret. GitHub Pages uses GitHub Actions
as its publishing source.

[Build contract](SPEC.md) · [Contributor instructions](AGENTS.md) ·
[Close API docs](https://developer.close.com/api/overview)
