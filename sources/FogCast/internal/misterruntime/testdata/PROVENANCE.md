# Runtime protocol fixture provenance

`protocol-v1-responses.jsonl` retains the protocol-1 comparison fixtures from
libmister-runtime `9a985d61cdd2d924bf72a44de2196cbb9ad814d8`.

`protocol-v2.jsonl`, `protocol-v2-edge-responses.jsonl`, and
`protocol-v2-persistence-responses.jsonl` are exact copies of the paired runtime
worker fixtures in `sources/libmister-runtime/tests/fixtures`; they are updated
alongside the runtime's protocol serializer. The FES parent checks their byte equality.
They cover nullable inspection persistence layout, active persistence mode,
durable data results, resumed save failure and retained unsafe recovery identity.
The response envelope permits an optional `core_data` only as an operation result.

`core-persistence-v1/records.json` is copied from mister-packages. The Go adapter
tests use the canonical record revisions and bounded payload values; record
encoding and publication remain runtime responsibilities.
