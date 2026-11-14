# Captured vendor-client live scan

`vendor-live-scan.txt` is the unchanged successful capture from
`out/nano-vendor-client-scan-20261108/trial/forced-scan.log` on TRWU8CBL.
Source checkout: `360098d8dfddb6cf1b6659a2cab58a92e2d93f8f`.
Both vendor client variants were loaded and the forced-scan symbol was bound
to `libjtag_client_dcrt.so`. No image programming, CPU reset or SD write occurred.

SHA-256: `b6aeb62a894c67923bf2588a4d6f07f08a1fd0be9e490cb01364932fcc75ace9`.
The dynamic private port and original line endings are preserved. This proves
one live scan, not programming readiness or native core-load acceptance.
