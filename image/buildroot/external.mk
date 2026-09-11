include $(sort $(wildcard $(BR2_EXTERNAL_FOGCAST_TARGET_PATH)/package/*/*.mk))

# Buildroot copies BASE_TARGET_DIR for each filesystem, then removes that copy
# outside fakeroot. Preserve sealed package modes in the image while making only
# the disposable copy's directories removable when the fakeroot command exits.
define FOGCAST_PACKAGE_ROOTFS_CLEANUP
	trap 'status=$$?; trap - EXIT; cleanup=0; "$(BR2_EXTERNAL_FOGCAST_TARGET_PATH)/board/fogcast-target/rootfs-package-cleanup.sh" "$(TARGET_DIR)" || cleanup=$$?; if [ "$$status" -ne 0 ]; then exit "$$status"; fi; exit "$$cleanup"' EXIT
endef
ROOTFS_PRE_CMD_HOOKS += FOGCAST_PACKAGE_ROOTFS_CLEANUP
