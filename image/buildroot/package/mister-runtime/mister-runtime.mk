################################################################################
#
# mister-runtime
#
################################################################################

ifneq ($(FES_PREBUILT_RUNTIME_SHA256),)
# Assembly consumes the explicitly selected binary. There is no build fallback.
MISTER_RUNTIME_SITE = $(FOGCAST_DIR)/bin
MISTER_RUNTIME_SITE_METHOD = local
MISTER_RUNTIME_LICENSE = GPL-3.0-or-later
ifeq ($(BR2_aarch64),y)
FES_RUNTIME_ARCH = aarch64
else ifeq ($(BR2_arm),y)
FES_RUNTIME_ARCH = armv7
else
$(error Prebuilt mister-runtime requires an ARMv7 or AArch64 target)
endif

define MISTER_RUNTIME_INSTALL_TARGET_CMDS
	printf '%s  %s\n' "$(FES_PREBUILT_RUNTIME_SHA256)" "$(@D)/mister-runtime-linux-$(FES_RUNTIME_ARCH)" | sha256sum -c -
	/bin/rm -f $(TARGET_DIR)/usr/sbin/mister-runtime
	$(INSTALL) -D -m 0755 $(@D)/mister-runtime-linux-$(FES_RUNTIME_ARCH) $(TARGET_DIR)/usr/sbin/mister-runtime
endef
else
FES_RUNTIME_SOURCE_PATH ?= .
MISTER_RUNTIME_SITE = /runtime-source/$(FES_RUNTIME_SOURCE_PATH)
MISTER_RUNTIME_SITE_METHOD = local
MISTER_RUNTIME_LICENSE = GPL-3.0-or-later
MISTER_RUNTIME_LICENSE_FILES = LICENSE

define MISTER_RUNTIME_BUILD_CMDS
	/bin/rm -rf "$(@D)/build"
	$(TARGET_MAKE_ENV) $(MAKE) -C $(@D) \
		CXX="$(TARGET_CXX)" AR="$(TARGET_AR)" NM="$(TARGET_NM)" \
		CXXFILT="$(TARGET_CROSS)c++filt" \
		MISTER_RUNTIME_VERSION="git-$(FOGCAST_MISTER_RUNTIME_COMMIT)" \
		all
endef

define MISTER_RUNTIME_INSTALL_TARGET_CMDS
	/bin/rm -f $(TARGET_DIR)/usr/sbin/mister-runtime
	$(INSTALL) -D -m 0755 $(@D)/build/mister-runtime \
		$(TARGET_DIR)/usr/sbin/mister-runtime
endef

endif

$(eval $(generic-package))
