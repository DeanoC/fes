# Native two-load window and SD return diagnostic

One bounded procedure on TRWU8CBL passed on native/firmware `5665860d60787910d3d651acdb34446714cb1898`
with unchanged FPGA producer `df234b524d0e1340b343df411c841bd1dc553817`. This is diagnostic
acceptance for repeated owned configuration and frame return. Ordinary core
loading and physical HDMI remain incomplete.

The first load reads and drains twelve changing full 3,686,400-byte frames,
closes at generation 7, resumes SD at generation 9, freshly retains the same
package and contains SD at generation 10. The second configuration reuses the
boot-pinned frame arena, reads and drains two frames and closes at generation
13 with final reader generation/cookie 3. SD stays contained after this return.
The trace preserves all sixteen CPU startup checkpoints. No native SD writes
or allocation releases are permitted.

Initial SD retention took 57.378s (100s bound).
First configuration/frame return took 44.261s
(45s bound); fresh SD retention/containment took 55.701s
(100s bound). Second configuration/frame return took
22.571s (45s bound). Each configuration
retains its 20s deadline; native and outer bounds remain 240s and 800s.

The private JTAG server is stopped, its original 15 MHz clock and exact USB
permissions are restored, and UART is free. The continuing exclusive claim and
spare card remain held; factory restoration was waived by the user. Seven
previous failed physical procedures remain separately retained. Earlier host
admission stops during a user cable/power change and motherboard USB hub fault
opened no devices and made no SD writes. The IT agent restored USB before this
fresh frozen procedure. There was one physical procedure and no retry.

The retained archive and all member checks are recorded in `retention.json`.
`hardware-replay.json` rejects damaged saved captures without another board run.
Software-only serial-based USB discovery was developed in a separate checkout;
it does not change this image or inherit its hardware acceptance.

Persistent native Tenfoot/window launch/return, ordinary package/ROM/parts/media
admission, physical HDMI, USB hub input and native networking remain open.

The initial saved-capture damage test found that the generic cold decoder
permitted an absent first activation log. The physical capture contains the
correct record, and the independent audit required it explicitly. A subsequent
host-only repair makes the explicit two-load decoder require both activations;
new regressions reject a missing, repeated, foreign-owner or borrowed second
activation. This changes no native or FPGA bytes and requires no board retry.
