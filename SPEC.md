# Close CLI v1

`close` is an independent Go CLI for Close CRM, published as
`github.com/voska/close`. It is not an official Close product.

## Contract

- JSON is the default on terminals and pipes. `--output table|tsv|ndjson`,
  `--json`, `--plain`, `--quiet`, and client-side `--fields` are explicit.
  List JSON preserves the API envelope and pagination metadata. Unknown fields
  and exact JSON numbers survive parsing. Diagnostics go to stderr.
- Noun-verb CRUD for leads, contacts, opportunities and tasks. Notes, calls,
  emails, SMS and custom activities live under `activity TYPE`.
  Configuration resources are read-only: pipelines, lead/opportunity statuses,
  custom fields (`--object`), users, sequences and smart views. Events support
  list/get. `me` reads the authenticated user.
- `list` accepts repeatable `--filter key=value`, `--limit` (default 50),
  `--skip`, `--all`. Events use `_cursor`; `search --data-file` uses body
  cursors on POST /data/search/. Pagination must fail loudly on non-progress
  or an API error; `--all` emits no partial aggregate on failure. NDJSON emits
  individual records after collection. It is not a live stream.
- `create|update` consume exactly one JSON object via `--data` or
  `--data-file PATH|-`. PUT sends only supplied fields. `delete` requires
  `--confirm`. All email/SMS writes and raw API writes require `--confirm`.
  `--dry-run` previews method, relative path, query and body without credentials
  or network activity. `--readonly` rejects mutations, including raw ones.
- Every typed write reads the individual object back without field projection
  or filters. Sent values must be a subset of observed values. Normalized
  values that cannot be compared are reported unverified. A successful delete
  requires GET returning 404. Email/SMS state proves persistence/queue state,
  never delivery. Raw writes always return submitted/unverified (exit 9).
- Never retry writes or follow redirects. Reads (including search POST) retry
  429/502/503/504 at most twice, honoring Retry-After and legacy rate_reset.
  Waits over 30 seconds return a retryable error instead of retrying early.
  Timeouts/cancelled writes have unknown outcomes (exit 9).
- Default origin is https://api.close.com/api/v1/. HTTP fixture overrides only
  accept literal loopback IPs and use environment credentials, never keychain.
  Absolute paths, traversal, encoding tricks, queries/fragments and control
  characters in endpoint arguments are rejected; query parameters use flags.
- Auth: CLOSE_ACCESS_TOKEN (Bearer), then CLOSE_API_KEY (Basic, empty password),
  then OS keychain. No plaintext secret file and no secret argument flags.
  `auth login --with-key-stdin`, `auth logout`, `auth status [--offline]`.
  `--account`/CLOSE_ACCOUNT selects an isolated keychain entry; explicit account
  selection with environment credentials is rejected to prevent targeting another
  organization. OAuth token
  provisioning/refresh is external. `auth status` checks GET /me/ by default.
- `schema [exit-codes]` exposes the actual command tree, flags and semantic
  exit codes. `version` exposes version, commit and build date.

## Exit codes

0 success, 1 general/protocol error, 2 usage/API validation, 3 reserved empty
result (empty lists succeed), 4 authentication, 5 not found, 6 forbidden,
7 rate limited, 8 transient read/network/server error, 9 submitted/unverified,
10 credential store/configuration error. Errors have a stable JSON error object
with code, type, message, optional HTTP status and request ID.

## Implementation and verification

Go 1.25+, Cobra for the command tree, go-keyring for OS credentials, stdlib
net/http, encoding/json and testing/httptest. Layout: cmd/close, internal/api,
internal/cli, internal/auth, internal/output, internal/errfmt. Test synthetic
HTTP fixtures before implementation, then `make ci` (format, vet, pinned lint, race tests,
coverage >=80% of internal packages, binary build) and a real executable smoke
test. PR and default-branch CI have read-only permissions and run on hosted
runners, including cross-platform GoReleaser snapshots and package manifests.
Tags run the full checks before a separate publishing job creates a GitHub
release and stable tags update the shared Homebrew tap and Scoop bucket. Only that job
has write permission and receives the package-repository credential. Builds
use Go 1.27.3 while retaining Go 1.25 source compatibility.

Sources checked 2026-11-03: [overview](https://developer.close.com/api/overview),
[pagination](https://developer.close.com/api/overview/pagination),
[rate limits](https://developer.close.com/api/overview/rate-limits), and the
[experimental OpenAPI](https://api.close.com/api/openapi.json). The API spec is
not used to generate incomplete request/response structs.
