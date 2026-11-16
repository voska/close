# Changelog

## Unreleased

- Request only the ID and written fields during verification, with a full-record
  fallback only when the endpoint explicitly rejects the field selector. Successful
  write stdout is the observed record, normally limited to those requested keys;
  read other fields separately if needed. If read-back fails or a create returns
  no usable ID, the unverified write echo is limited to `id` (or an empty object
  if absent). Reject empty body keys and keys containing commas or whitespace
  before typed writes (exit 2).
- Add root-owned deployment policies for content families, destructive actions,
  raw API access and optional read-only deployments; refusals exit 11 with a
  named rule. Include a restricted-agent sample in release archives. Content
  restrictions require explicit task/lead projections and refuse embedded tasks.
  Task reads and `task.*` search projections use the metadata allowlist documented
  in README. Task `text` is allowed only on lists with exactly one `_type=lead`
  and no `_type__in`, or in `task.lead` search projections. Task writes may set
  `text`; policy-refused verification uses one ID/lead-pinned list read.
  Search conditions on `text` remain refused. Other content checks still refuse
  previews, subjects, recordings and attachments.

- Add contributor instructions, shipped Close agent skill and command reference,
  and static GitHub Pages docs with llms.txt and skill downloads.
- Add tested release snapshots, tagged GitHub releases, and automatic Homebrew
  and Scoop updates through the shared voska package repositories.
- Run the pinned golangci-lint checks locally and in CI.
- Add CRM commands for leads, contacts, opportunities, tasks and activities.
- Add configuration discovery, event cursors, advanced search and raw API access.
- Add OS keychain and environment authentication, JSON/TSV/table/NDJSON output,
  command introspection, dry runs, read-only mode and verified writes.
