# Native menu library shell on the designated kit — 2026-09-28

Classification: **exact-artifact hardware diagnostic pass** for the stacked
FogCast shell and previously qualified menu scanout. This was an explicitly
selected development menu, not product-image selection or release acceptance.

The shell source was FES commit `40e7615bf6118f6572b4f9a57a2465773cbbc0b`,
stacked on scanout commit `e54dc70c`. The ARMv7 `fogcast-kit` binary built
from that source had SHA-256
`2cbedf4a9a4e0ee10b5c1a9bbcdf4f89bc3929a6b5f9da48952f15223db9c506`.
The kit booted the retained diagnostic image
`052a39fd9dbdf65909618bee03ba2d01577431b044591a7b612d346c526b702f`
and the sealed `fes.menu` package
`0d1ecd3328237fb4ba93e69c69dee45f48b4251a063e54cc86e6f7a8c96b1cc2`
(RBF SHA-256
`839b4084851c7be180fbcc6612c22dd2dab546bb5fcb583e87fe732b4f7dde28`).
The runtime DDR boot record was `latched`. The diagnostic image and menu
package are detailed in
[the scanout kit record](2026-09-28-native-menu-kit-presentation.md).

Under the kit lease, a one-frame pattern first confirmed the package and
socket: generation 1, displayed sequence 1, zero underflows. The exact
`fogcast-kit` binary then ran with `--menu-display --no-transition`, using the
existing launcher configuration. The FogCast host service was offline, so the
library shell displayed its cached platform rows and the explicit offline
state. A settled HDMI capture showed the FogCast platform wheel, and the
operator confirmed that the connected controller moved its highlight. Runtime
status remained `Available=true` at generation 1 with 1280×720 RGBA geometry;
displayed sequence advanced through 208, 368 and 422 with zero underflows.
The kit process reached 36,748 KiB high-water RSS with nine threads.

The first MJPEG capture frame was corrupt and initially retained the earlier
pattern; a settled capture showed the actual shell. The PNG is retained at
`out/validation/native-menu-shell/kit/menu-shell-settled.png` in this worker
checkout. This test establishes physical input-to-render behavior and
continuous scanout while the host is offline. It does not establish online
game launch, host federation or a product-image default menu.

The temporary launcher stopped, the menu session was stopped and its kit
lease was released. The original baseline image
`0103b5f04cbe256ed84e97d589ec5cf108b1031e415820bd28c8bd3cb16b1a4d`
was then restored with the supported appliance updater. The confirmed boot ID
was `bc55b635-38d3-47be-8708-b8a55da8c72f`; updater status reported
`trial=false`, `raw_idle_ready=true`, no pending image, and the kit lease free.
