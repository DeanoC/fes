# Physical-controller kit diagnostic

The operator completed a controller test on kit A with accepted bundle
`67afde612b5d12cd18aa68485e81b524a56c0b55fc7c06b05dd81bf8906de080`,
from source `d0ca0f3e`, at selection generation 12. This is the same bundle
used by [the multi-host diagnostic](https://github.com/DeanoC/fes/pull/739).
No software was deployed or restarted during this test, and the assistant sent
no synthetic input or launch/Stop mutations.

The operator navigated to [Installed cores](2026-10-16-physical-controller/installed-cores.png),
selected FES Pong and was asked to press A/Cross, then hold Up and Down with a
release between them. The operator reported “Its working”. The 45-second HDMI
recording shows Pong and left-paddle movement: the sampled upper edge ranges
from y=73 to y=757 in the 1920×1080 capture. See the
[upper](2026-10-16-physical-controller/paddle-upper.png) and
[lower](2026-10-16-physical-controller/paddle-lower.png) samples and
[measurements](2026-10-16-physical-controller/physical-video-analysis.json).
The recording does not independently isolate both directional presses or
instrument the controller's electrical events; the controller model was not recorded.

After the requested Select+Start hold, the operator reported returning to the
menu they launched from. The [returned screen](2026-10-16-physical-controller/returned-menu.png)
confirms Installed cores with FES Pong selected. A subsequent authenticated
[status observation](2026-10-16-physical-controller/final-ready.json) confirms
ready, native idle and a free lease, with unchanged boot, base and generation.
Exact button hold timing was not instrumented.

The initial [Pong library row](2026-10-16-physical-controller/pong-launch-failed.png)
was a different catalog entry marked unlaunchable by the host. It was not the
installed-core tile. That confusing discoverability issue remains; this test
neither deletes library entries nor introduces title-based core aliases.
Generation-checked local Stop also remains a separate hardening task; a
successful controller chord does not prove stale-client Stop safety.

[Evidence identities](2026-10-16-physical-controller/evidence.json) bind the
public snapshots and the retained recording under
`out/validation/physical-controller/physical-launch-input.mkv` in this worktree.
The video itself is not included in the repository. No production code or
shared contract changed.
