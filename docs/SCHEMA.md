# Snapshot Schema

`agent-observer --dump-json` emits a `WorldSnapshot`.

The top-level `schema_version` field is the compatibility marker for downstream tools. The current version is `v1`.

## Compatibility

- Additive fields may be introduced in `v1`.
- Existing field names and meanings should not change within `v1`.
- Removing fields, changing status values, or changing object identity rules requires a new schema version.

## Identity

- Task IDs are `<batch_id>:<task_index>`.
- Batch IDs come from the local Claude tasks directory name.
- `source_path` points at the observed local file unless `--redact` is used.

## Privacy

Snapshots can include local paths, task titles, prompts, warnings, and inferred active files. Use `--redact` before sharing output outside the local machine.

Telemetry events are separate from snapshot JSON. They use their own `schema_version` and only include aggregate counts, timing, version, platform, command mode, and error category metadata.
