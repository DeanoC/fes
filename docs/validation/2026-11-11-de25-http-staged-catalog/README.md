# HTTP delivery of staged installer catalogs

The previous HTTP downloader selected playable entries through CORESEL.JSN and UI.JSON, while the current installer export places available packages in INSTALL.JSN and may start with an empty library. ProgramSource now downloads that installer catalog, retains the entire batch, and applies the backend platform admission before exposing any install plans. SD and HTTP use one catalog decoder. Publication and owned SD installation remain separate operations.

The existing record, per-file, aggregate content and request bounds remain in force. HTTP checks each staged checksum and rejects redirects, transformed responses, missing members, corrupt later content, duplicate identities, incompatible platform metadata and cancellation without publishing a partial batch or touching devices. Networking has one 100-second outer operation bound. Downloaded bytes are privately retained and remain usable after the server disappears.

Validation on the source files hashed in result.json:

- Full nativefabric and packagestore Go suites passed.
- make test-changed against 9a9590eb8 passed, including 1,956 parent tests (three skips), generated-consumer consistency and the native runtime integration selection.
- The HTTP integration test downloads two actual staged fixture exports, closes the server, mutates export buffers, installs both through the host client, runs second/first/second launch-return cycles, and rediscovers both installed records through a fresh model store. Ten malformed/failed transfer cases preserve the previously admitted catalog and perform no device IO.
- An initial full-suite failure exposed a test fixture that rebuilt its retained selection after its source buffers had changed. The test now preserves the original admitted plan; production validation was unchanged. Failed and successful logs remain under out/nano-http-staged-catalog-20261111.

This is host/model evidence. It does not qualify native HTTP, Ethernet MAC setup, PHY/NoC/SMMU behavior or DMA drain. Native Ethernet stays disabled. The changed shared SD catalog path also requires a new native SDK and matched entry/BL31/U-Boot composition, followed by fresh source admission before hardware testing. The prior USB-startup stop remains unresolved and is not retried by this change.
