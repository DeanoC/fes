# 850 HPS I2C location from QSF

`make sim EXP=850_hps_location` checks the HPS GP signature. `make oss
EXP=850_hps_location` places `hdmi_i2c` from QSF `HPS_LOCATION
HPSINTERFACEPERIPHERALI2C_X52_Y60_N111` at
`cyclonev_hps_interface_peripheral_i2c.52.60.0`. RTL has no BEL attribute.
Quartus comparison is not implemented.

GPI signature `0xD850`, payload `0x00A6`.

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
