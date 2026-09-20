# Runtime protocol fixture provenance

`protocol-v1-responses.jsonl` retains the protocol-1 comparison fixtures from
libmister-runtime `9a985d61cdd2d924bf72a44de2196cbb9ad814d8`.

`protocol-v2.jsonl`, `protocol-v2-edge-responses.jsonl`, and
`protocol-v2-persistence-responses.jsonl` are exact copies of the runtime worker
fixtures from libmister-runtime `8ebfea9adc1a582ef0d41da5f4310afc0ab312b6`.
The FES parent selects the paired runtime/FogCast commits and checks their byte equality.
They cover nullable inspection persistence layout, active persistence mode,
durable data results, resumed save failure and retained unsafe recovery identity.
The response envelope permits an optional `core_data` only as an operation result.

`core-persistence-v1/records.json` is copied from mister-packages. The Go adapter
tests use the canonical record revisions and bounded payload values; record
encoding and publication remain runtime responsibilities.
