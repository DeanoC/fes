# Task 6 Report: Host V2 Cache Transport

## Outcome

Implemented the accepted Task 6 Host V2 Cache Transport from base
`528e4ddecefcb8692f10fdece26257c42bce8a87`.

Implementation commits:

- `1f985981e53080c485583867aaa0ada01d2016a8` — `feat: add authenticated v2 content client`
- `81f3cedca4ce697eb2039db5a888ddec18440727` — `fix: reject invalid cache upload results`

The target cache inventory, eviction, reboot persistence, and launch integration remain later plan tasks (Tasks 7 onward) and were not added to Task 6.

## Files Changed

- `host/content_client.go` — authenticated probe/upload/content-launch methods; request validation; exact response confirmation; one-shot upload body; typed privacy-safe transfer errors.
- `host/content_client_test.go` — request, response, validation, replay, failure, privacy, and context contract coverage.
- `host/client.go` — shared safe endpoint construction and bounded response/error-envelope decoding while retaining the v1 wrapper behavior.

`progress.md` was not edited.

## TDD Evidence

### RED 1: v2 client surface and transport contract

Command:

```text
mise exec go@1.26.5 -- go test ./host -run 'Content|Upload|Probe' -count=1 -v
```

Result: exit 1. Compilation failed at every new call site because
`ProbeContent`, `UploadContent`, and `LaunchContent` were undefined. This was the expected feature-missing failure.

After the minimum implementation, the same command exited 0 and all new focused tests passed.

### RED 2: nested launch-system substitution

Command:

```text
mise exec go@1.26.5 -- go test ./host -run 'TestContentClientRejectsMismatchedResponses/launch_status_system' -count=1 -v
```

Result: exit 1 with `mismatched response accepted`. The client was then changed to require both the nested returned status system and returned content identity to equal the request. The same command subsequently exited 0.

### RED 3: invalid upload result enum (review finding)

Command:

```text
mise exec go@1.26.5 -- go test ./host -run 'TestContentClientRejectsMismatchedResponses/upload_(missing|unknown)_result' -count=1 -v
```

Result: exit 1; both `upload_missing_result` and `upload_unknown_result` failed with `mismatched response accepted`. `UploadContent` was then restricted to `present` or `created`. The same command subsequently exited 0 for both cases.

## Acceptance Review

- Exact methods/routes: `GET,PUT /v2/cache/{system}/{sha256}?extension=...` and `POST /v2/launch` are asserted through real `httptest.Server` requests.
- URL safety: system, digest, extension, size, and launch game ID are validated before endpoint construction; inherited base paths, queries, and fragments are discarded; query construction uses `url.Values`.
- Authentication: every v2 endpoint sends the existing bearer token; v1 health remains unauthenticated.
- Upload contract: exact `application/octet-stream` and `Content-Length` are asserted. The body wrapper exposes only `Read`, leaves `Request.GetBody` nil, and a failing transport proves one call/one read sequence with no replay.
- Launch contract: exact JSON and `application/json` are asserted.
- Response safety: success and error bodies retain the shared 1 MiB bound; API error envelopes decode to `*protocol.APIError`; absent probe is a normal 200 response.
- Confirmation: present probe, upload, and cached launch reject substituted system/content; upload rejects missing/unknown results.
- Failure behavior: upload transport/body-read failure becomes `TRANSFER_FAILED`, preserves the underlying read error for `errors.Is`, and displays no private source path.
- Contexts: probe and launch retain their caller request context; upload independently retains a longer caller upload context.
- Compatibility: the shared refactor keeps v1 paths, authentication exception, JSON content type, response limit, typed errors, and serialization behavior unchanged. Full v1 and repository suites passed.
- Privacy: tests use synthetic bytes only. No token, ROM bytes, NAS path, staging path, or target cache path is added to errors, logs, fixtures, or Git artifacts.

## Verification Commands and Results

All commands ran from `/Users/clawzai/Developer/mister-remote/.worktrees/poc1a`.

```text
mise exec go@1.26.5 -- go test ./host -run 'Content|Upload|Probe' -count=20
```

Exit 0: repeated focused suite passed.

```text
mise exec go@1.26.5 -- go test -race ./host -count=1 -v
```

Exit 0: complete host suite passed under the race detector, including existing v1 tests.

```text
CGO_ENABLED=0 mise exec go@1.26.5 -- go test ./host -count=1
```

Exit 0: CGO-free host tests passed.

```text
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 mise exec go@1.26.5 -- go test -c -o /dev/null ./host
```

Exit 0: CGO-free Linux/ARM64 host test binary compiled.

```text
mise exec go@1.26.5 -- make check
```

Exit 0 after initial implementation and again after the review fix. The final run completed `gofmt`, `go test -race ./...`, all repository shell/image policy tests, and `go vet ./...` successfully.

```text
git diff --check 528e4dd..HEAD
```

Exit 0 with no whitespace errors.

## Independent Review

The first read-only review found one Important issue: an omitted or unknown upload result enum was accepted. Commit `81f3cedca4ce697eb2039db5a888ddec18440727` fixed it through the third RED/GREEN cycle. The narrow re-review returned `Clean/Ready` and found no remaining Critical, Important, or Minor issues.

## Concerns

No open Task 6 concerns. The initial unprivileged test attempt could not bind an `httptest.Server` loopback port due to the workspace sandbox; rerunning with the required local test permission passed and all recorded RED/GREEN failures above were code-contract failures rather than sandbox artifacts.
