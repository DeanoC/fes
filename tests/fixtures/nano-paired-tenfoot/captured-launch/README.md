# Captured native launch; offline replay only

`initial.uart` and `launch.uart` are unchanged byte slices of the TRWU8CBL
capture from operator source `03c947d15173efb65befcccd74bef71fba0c8010`, native
producer `f8653867f`, boot ID `aa11ddf99761b275`. The original procedure failed
at its USB/fetch observer ordering check; return and shutdown were not tested.
That result remains failed.

`contract.json` retains the exact required compiled-contract fields, source
receipt identities, selected programs and original receive offers/times. Its
pure menu contract was compared with the full SDK record before export. No
owner, identity, generation, line ending, or firmware row in the UART slices
was changed. The local attributes preserve the original bytes through Git.
The source console and original result hashes bind these slices to the retained
failed attempt in `out/nano-menu-usb-board-20261108` and its evidence archive.

The regression parses actual cold boot, pins, menu frames/drain, configuration,
core opening, USB boundaries and advancing fetch before accepting this one
recorded launch response. An injected exchange replays the original partial
receive offers and elapsed times; it opens no device and sends no real command.
Separate mutations exercise invalid ordering/ownership and permanent retirement.
Passing replay does not turn the original trial into a full-cycle or physical
acceptance result.
