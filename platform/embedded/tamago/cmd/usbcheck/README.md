# Nano owned USB boot check

This explicitly selected, bounded program verifies the cold boot context and
sealed HPS pins before constructing the firmware USB client. It acquires the
prepared lease through the Nano BSP, starts the actual DWC2 boot-keyboard driver
and confirms live PHY/VBUS readiness before polling reports for five seconds.
It then requests one checked stop and retains all objects until reboot.

The entry checks the exact native link assignment and presence of the selected
client/driver path. BL31 installs the physical callbacks only when its matched
cold profile selects this program. The base probe profile still declares USB
contained at entry. A driver or firmware failure produces a failure record;
there is no direct-access fallback, automatic retry, SD or core launch.

Direct boot keyboards are supported by the driver model. Hubs, controllers and
combined USB/storage/core ownership still require implementation/qualification.
This build supplies no physical USB acceptance.
