# Runtime protocol fixture provenance

These exact protocol response fixtures are copied from libmister-runtime commit
`9a985d61cdd2d924bf72a44de2196cbb9ad814d8` after the final Task 8 review and
the reviewed two-interface serializer-fixture correction:

- `protocol-v1-responses.jsonl`
- `protocol-v2.jsonl`
- `protocol-v2-edge-responses.jsonl`

They freeze the strict protocol-1 compatibility projection and the complete
12-field protocol-2 envelope consumed by FogCast. Update them only from a
reviewed runtime contract revision and compare the bytes in tests.
