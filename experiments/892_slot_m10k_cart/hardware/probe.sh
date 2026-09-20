#!/bin/sh
# Diagnostic probe for the cart fragment alone. Overlay onto 891 before a
# meaningful kit test; this only checks the 0xD892 signature if the cart RBF
# is loaded as a full chip.
set -eu
signature=55442
status=$(busybox devmem 0xFF706014 32)
[ "$(((status >> 16) & 65535))" -eq "$signature" ] || {
    echo "FAIL: missing cart signature $status" >&2
    exit 1
}
echo 'PASS: cart fragment signature'
