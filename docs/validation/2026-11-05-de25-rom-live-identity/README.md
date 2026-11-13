# Retained ROM identity in native observations

ROM preflight now installs the verified private activation receipt in the
running `PackageIdentity.ActivationReceiptSHA256`. It was previously recorded
only in the separate cached service field. The Tenfoot observer accepts that
field only when it matches the identity. The direct console publishes the
receipt too, and its client requires the expected receipt on launch and a stable
receipt on later controls. Return clears the published identity and receipt.

The session API copies the ROM's ID, firmware role, source size and digest from
the private retained receipt into its existing ROM-link observation. It does not
reconstruct them from mutable catalog or storage data. Caller mutation cannot
change the cached source observation. This is content identity, not a new
firmware capability, bridge grant or independent live ROM-completion proof.

Two firmware selections can share one unchanged FPGA package and carry distinct
activation identities. The full nativefabric race suite passes; a new backend
test launches ROM A, ROM B and ROM A through the actual retained store/loader,
arena, service and host-client API, with actual C descriptor decoding and modeled
firmware. The console test checks publication and clearing. The new Tenfoot
observer fixture completes the same three-cycle pattern and rejects duplicate,
missing, changed and malformed receipts. The direct client retires on receipt
loss/change and refuses another command. All 38 selected observer tests passed.

The recovery uploader now also permits `firmware-alt` at the fixed
`FES/FIRMALT.BIN` path, with the same exact 4/8 KiB bounds and existing protected
file, readback and failure rules. All 50 selected upload/ROM/composition tests
passed with one host-Go-version skip; that linker test separately passed with
Go 1.26.5. The prior SDK's actual content also preflights through the new loader
without firmware access. No hardware ran. The changed runtime needs a freshly
matched native chain before a bounded board attempt; the preceding image's
offline evidence does not qualify this changed runtime.
