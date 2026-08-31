# FogCast development

## Local checks

FogCast uses Go for the host and target services and dependency-free
JavaScript tests for the browser UI.

```sh
go test ./...
node --test internal/hostapi/ui_metadata_test.js internal/hostapi/ui_app_test.js
go vet ./...
git diff --check
```

The browser integration suite can be run separately with:

```sh
node --test internal/hostapi/ui_browser_test.js
```

## Builds

Build the normal project binaries with:

```sh
make build
```

Important outputs include the host/API programs, CLI and hardware test tools,
remote-media helpers, and the ARMv7 target agent. Build only the target agent
with:

```sh
make build-agent
```

The resulting target binary is:

```text
bin/mister-agent-linux-armv7
```

## Private configuration

Target addresses, credentials, tokens, game-library paths, and private media
configuration stay in untracked local configuration. Examples in source must
use placeholders rather than live values.

The dedicated MiSTer Pi is disposable development hardware on a local
network. Its normal development login is `root` with password `1`, and its SSH
host key may change after a rebuild. Rebooting, reflashing, or replacing its
image is acceptable. Do not build production security, rollback, or failover
systems around this fixture.

## Hardware changes

Before changing target behavior, trace the relevant path from
`docs/ARCHITECTURE.md` into the named source files. Test host-only behavior
locally, then run the actual operation on the designated MiSTer Pi when the
feature touches FPGA loading, input, video, audio, or target lifecycle.

The current development-RBF goal will reuse `/dev/MiSTer_cmd` and the resident
Main-compatible process. A development core may make Main exit or leave the
screen unusable; a target reboot is an acceptable recovery during this work.
