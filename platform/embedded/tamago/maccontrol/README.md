# Fixed MAC firmware lease client

The client consumes an explicitly bound secure MAC/window lease with a private
peripheral generation. It does not share SD or FPGA bridge generations, choose
addresses, initialize firmware or qualify a physical platform. Copies share
operation serialization, the last receipt, retirement and exclusive binding.

`New` requires the trusted boot owner, contained generation zero and the exact
pinned-window/private-generation capabilities. `Acquire` grants stopped
controller access; the driver initializes its descriptors and verifies its PHY,
then `Confirm` obtains live firmware confirmation before packet workers start.
`BindPlatform` transfers that confirmed lease once to `xgmac.Device.BindControl`.
Raw public aliases then lose mutation/observation access. The driver consumes
Start and Confirm separately on each restart and returns complete Stop/Start
controls to the network controller. An unconsumed Confirm hook is rejected by
the network controller, rather than allowing traffic before confirmation.

Driver stop acknowledgements precede firmware Stop, whose trusted callback must
independently establish interconnect drain and containment. A successful Stop
permits no descriptor/MMIO access until a new Acquire. Query and an already
contained Stop preserve generation, while acquire/confirm/active stop each
advance it. Capacity for confirmation and stop is reserved before acquisition.
Neither a reset bit nor a caller-supplied address/readiness value grants a lease.

Unknown, failed, lost or malformed replies retire the shared client without
retry or replacement; the last known receipt and DMA allocation remain retained.
A complete reply received during cancellation records known ownership before
returning cancellation. Reconstructing a client cannot adopt an active or used
lease. The driver/controller still closes traffic after a cancelled boundary.

The portable tests execute the real owned C firmware dispatcher and engine through a host pipe,
with explicitly modeled authority/grant/drain callbacks and corrupt/lost reply
cases. The owned firmware export routes the channel, but its default setup has
no qualified MAC boot supplier and returns NotReady. Native Ethernet stays disabled. See the
[shared contract](../../../../sources/mister-packages/docs/embedded-mac.md).
