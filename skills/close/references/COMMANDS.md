# close command reference

Use `close schema [COMMAND ...]` and `close COMMAND --help` for the installed
version's exact flags. All commands are noninteractive. JSON is the default.

## Command groups

| Command | Actions |
| --- | --- |
| `lead`, `contact`, `opportunity`, `task` | `list`, `get ID`, `create`, `update ID`, `delete ID` |
| `activity` | `list` across all activity types |
| `activity note`, `activity call`, `activity email`, `activity sms`, `activity custom` | `list`, `get ID`, `create`, `update ID`, `delete ID` |
| `activity meeting` | `list`, `get ID` |
| `pipeline`, `lead-status`, `opportunity-status` | `list`, `get ID` |
| `custom-field` | `list`, `get ID`; requires `--object` |
| `user`, `sequence`, `smart-view`, `event` | `list`, `get ID` |
| `me` | Current authenticated user |
| `search` | Advanced JSON filtering (`POST /data/search/`) |
| `api METHOD PATH` | Relative Close JSON endpoint |
| `auth login`, `auth status`, `auth logout` | Verify/store, check or remove a keychain API key |
| `schema [COMMAND ...]`, `schema exit-codes` | JSON command tree or semantic exit codes |
| `version`, `--version` | Build metadata or version string |
| `completion bash\|zsh\|fish\|powershell` | Shell completion scripts |

## Global flags

| Flag | Meaning |
| --- | --- |
| `--json` | JSON output, already the default |
| `--output json\|table\|tsv\|ndjson` | Explicit output format |
| `--plain` | TSV without headers |
| `--quiet` | IDs only; bare version for `version` |
| `--fields id,display_name` | Local projection; dotted paths and literal custom-field keys supported |
| `--account NAME` | OS keychain account, default `default` or `CLOSE_ACCOUNT` |
| `--readonly` | Reject CRM mutations; also `CLOSE_READONLY=1` |
| `--timeout 30s` | Per-request timeout, including response body |
| `--no-input` | Explicit noninteractive mode; all commands already avoid prompts |
| `--no-color` | Accepted; all output is already free of terminal color codes |

Choose one of `--json`, `--plain` or `--output`. `CLOSE_JSON=1`/`CLOSE_PLAIN=1`
set defaults. Explicit keychain account selection conflicts with either
`CLOSE_ACCESS_TOKEN` or `CLOSE_API_KEY`.

## Lists and individual reads

`list` returns one page unless `--all` is passed. List flags:

| Flag | Meaning |
| --- | --- |
| `--filter key=value` | Repeatable URL-encoded query parameter |
| `--limit N` | Records per page, default 50; endpoint limits still apply |
| `--skip N` | Initial offset; events use cursors instead |
| `--all` | Collect all pages before printing; no partial aggregate on failure |
| `--api-fields id,name` | Request `_fields` from Close |

`get ID` accepts `--api-fields` too. Use `--limit`/`--skip` rather than `_limit`
or `_skip` filters on typed lists. `event list` uses `--filter _cursor=TOKEN`.
Events cover 30 days; offset deduplication does not provide snapshot isolation.

Under a `message-content` deployment policy, `task` and `lead` `list`/`get`
exit 11 unless `--api-fields` names the fields to fetch, with no empty items,
for example `id,_type,date,is_complete,lead_id` or `id,display_name`. `--fields`
is local and does not narrow the request. `subject`, `body_preview`,
`voicemail_url`, `recording_url`, `attachments` and a lead's embedded `tasks`
are refused in every projection, search included, and as request-body keys.

Under that rule a task projection, in `task list`/`get` or for any `task`
object in a search, may name only these fields:

```text
id, _type, lead_id, lead_name, contact_id, contact_name, assigned_to,
assigned_to_name, created_by, created_by_name, updated_by, updated_by_name,
date, due_date, date_created, date_updated, is_complete, is_dateless,
priority, organization_id
```

Task `text` is the message body on inbound SMS tasks, so it can be read only
for lead tasks: add it to a `task list` that has exactly one
`--filter _type=lead` and no `_type__in` filter, or to a `task.lead` search
projection. `task get` never returns it. A search condition on `field_name`
`text` is refused for any object.

`custom-field --object` accepts `lead`, `contact`, `opportunity`, `activity`,
`shared` or `custom_object_type`.

```sh
close --readonly lead list --all --api-fields id,display_name --fields id,display_name
close --readonly contact list --filter lead_id=lead_demo
close --readonly task list --filter _type=lead --api-fields id,lead_id,text,date
close --readonly activity list --filter _type=Note
close --readonly custom-field list --object contact
close --readonly event list --filter object_type=lead --filter action=updated
```

## Typed mutations

`create`/`update ID` take exactly one object via `--data JSON` or
`--data-file PATH|-`. Updates send only supplied fields. A write whose
top-level keys are empty or contain a comma or whitespace exits 2 before it is
sent. `--dry-run`/`-n` previews method, path, query and body with no network or
credential requirement. `--readonly` rejects all CRM mutations, including dry runs.

Deletes require `--confirm`; lead deletion cascades to related CRM records.
Every email/SMS mutation requires `--confirm`, including draft creation/update.
Stored or queued state never proves delivery. There is no automatic write retry.

Successful create/update requests only `id` and supplied top-level keys during
read-back, compares their values and prints that observed record. A full-record
fallback requires an explicit HTTP 400/422 rejection of the `_fields` parameter
and stays subject to deployment policy; a refused fallback exits 9.
Server normalization may make this inconclusive. Exit 9
means submitted/unverified or unknown outcome; inspect stdout plus the stderr
error before retrying. On a mismatch stdout is the observed record. When
read-back fails or is refused, or a create returns no usable ID, stdout is only
the `id` from Close's write response, or `{}` if it has none, unverified.
Under `message-content`, task writes may set `text`. Verification reads the
`task` list pinned to the written ID and `_type=lead` and requires exactly one
task. An empty list after a create triggers up to two retries over five seconds.
Successful deletion verifies
an individual GET returning 404. Repeated deletion reports
`deleted: false, already_absent: true` after verifying absence.

```sh
close task update task_demo --data '{"is_complete":true}' --dry-run
close activity note create --data-file note.json --dry-run
close lead delete lead_demo --dry-run
```

## Search and raw API

```sh
close --readonly search --data '{"query":{"type":"object_type","object_type":"lead"},"_fields":{"lead":["id","display_name"]},"_limit":100}' --all
close --readonly api GET /webhook/ --filter _limit=50
close api POST /report/activity/ --data-file report.json --dry-run
```

Search flags: `--data`/`--data-file`, `--all`. Wrap Close's Copy Filters object
in `query`; without `_fields`, results contain only IDs and object types.
Search cursors expire after 30 seconds; Close caps results at 10,000.

`api` supports GET/POST/PUT/PATCH/DELETE and takes `--filter`, `--data` or
`--data-file`, `--all` on reads, `--dry-run`, and `--confirm` on mutations.
Use relative paths and `--filter` for query parameters. The origin is fixed
to `https://api.close.com/api/v1/`; redirects are rejected. Multipart is unsupported.
Raw mutations always exit 9 after submission. POST `/data/search/` is a read;
other POST endpoints, including reports, take the conservative raw-write path.

## Authentication

- `auth login --with-key-stdin`: verify `/me/`, then store the supplied key
  in the OS keychain and read it back. Production origin only.
- `auth status`: verify selected credentials against `/me/`.
- `auth status --offline`: inspect availability with `verified: false`.
- `auth logout`: remove and verify removal of the selected stored key;
  environment credentials and remote keys remain active.

Credential precedence: `CLOSE_ACCESS_TOKEN`, `CLOSE_API_KEY`, then OS keychain.
OAuth refresh is external. There is no plaintext credential file fallback.
Linux keychain use requires Secret Service. Inject secrets through the caller's
secret manager rather than putting them in shell history or arguments.

## Errors and retries

Errors are stderr JSON: `{"error":{"code":4,"type":"authentication","message":"..."}}`.
`http_status` and `request_id` may also be present. stdout remains data only.

0 success; 1 general/protocol; 2 usage/API validation; 3 reserved (empty lists
exit 0); 4 authentication; 5 not found; 6 forbidden; 7 rate limited;
8 transient read/network/server; 9 submitted/unverified or outcome unknown;
10 credential store/configuration; 11 deployment policy refusal with a named
`rule`. `close schema exit-codes` is authoritative. Deployment policy can refuse
raw API and aggregate activity/event examples above; prefer typed note/custom
activity reads when those families are permitted.

Reads retry 429/502/503/504 at most twice and respect `Retry-After`/`rate_reset`.
Delays over 30 seconds return to the caller. Writes never retry automatically.
SIGINT/SIGTERM cancel requests and retry waits.
