# Paired kit launcher connection

The host process can serve a second, authenticated HTTP listener for a kit
launcher. The normal browser API remains bound to loopback and retains its Host
header restriction. Both listeners share the same library services, session
coordinator, and target/input lease owner.

Pass `--launcher-config /absolute/private/launcher-host.json` to `fogcast-api`.
The file is a regular file readable only by its owner, containing:

```json
{
  "listen": "0.0.0.0:8789",
  "token": "a-separately-generated-random-secret-of-at-least-32-characters",
  "target_id": "73dc9f5f-1a12-4a95-a820-a9b4e600769a"
}
```

The listen address uses a literal IP and explicit port. The token is distinct
from the host-to-agent token; it is not written to public settings or logs.
Configuration is opt-in, read at host startup, and enables remote input
composition so successful FPGA launches attach the existing input bridge.
Generated media provisioning supplies the corresponding API URL, token, and
identity to the kit; the launcher does not obtain a kit lease credential.

Each request sends `Authorization: Bearer <token>` and
`X-FogCast-Target-ID: <target_id>`. The configured selected target must match the
paired identity. Target settings updates and admitted launcher operations are
serialized so an address/selection edit cannot redirect an in-flight launch.
An absent host remains a client reconnect state; this is not an offline library.

Allowed operations are:

- `GET /api/v1/games`, `/api/v1/platforms`, `/api/v1/health`, `/api/v1/status`.
- `GET /api/v1/presentation/games/{id}` for a validated catalog game ID (cover
  handles, `logo_id`, studio, screenshots).
- `GET /api/v1/presentation/artwork/{handle}` for 64-hex catalog or metadata cover and logo handles.
- `GET /api/v1/session` and `/api/v1/session/input`.
- `POST /api/v1/session/launch` with the existing `{"game_id":"pong"}` body.
- `POST /api/v1/session/stop` with no body.
- `POST /api/v1/launcher/input?session_id=<session_id>` for controller input.

Responses for the existing routes retain the existing public API schema. There
are no settings, filesystem, development-RBF, input-detach, or arbitrary proxy
routes on the launcher listener. Missing/invalid authentication returns 401,
identity mismatch returns 403, and unavailable routes return 404.

## Controller stream

An attached input status contains `session_id`, a non-secret hexadecimal
attachment identifier, and optionally `source` (`launcher` or `desktop`). The
identifier grants no bridge authority. A successful game launch includes this
input status in its response; the launcher can also read current session/input
status. A subsequent launch/input attachment rotates the identifier, including
when the same game launches again.

Open the input POST with a streaming request body. The host supports HTTP/1.1
full duplex and immediately flushes a single response line:

```json
{"ready":true,"session_id":"..."}
```

The request body is newline-delimited JSON. Send normalized `remoteinput.Event`
values (the same numerical enums used by the existing bridge client):

```json
{"event":{"Device":1,"Kind":1,"Action":1,"Code":104,"Value":0}}
{}
```

The first example presses gamepad A; `{}` is a heartbeat. Send a heartbeat at
least every 250 ms while attached. The host permits only known gamepad buttons
and axes, bounds each line to 4096 bytes, and ends the stream after one second
without a complete line. Keyboard injection is not part of this endpoint.

A source is exclusive. Another stream, or a desktop producer that already sent
input, causes 409; the launcher does not detach or take over that source. While
a launcher is attached, ordinary desktop `SendEvent` calls return busy.

Close the request body when the controller disconnects, the session changes,
or the launcher exits. On EOF, timeout, malformed input, or cancellation, the
host closes the source's authenticated bridge transport; the bridge releases
held controls and the host discards its input snapshot. A later source can
reconnect the same bridge attachment, but no old held state is replayed. Delayed
events and delayed cleanup from an old source cannot affect its replacement or
a later game. The stream connection is closed rather than reused.

The launcher must monitor stream completion, re-read current session status,
and establish a fresh source after reconnect. It must discard queued input
across that boundary. Stop remains an ordinary session operation: failures
must remain visible and retain the retry path.
