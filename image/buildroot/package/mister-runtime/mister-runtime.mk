################################################################################
#
# mister-runtime
#
################################################################################

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
	$(INSTALL) -D -m 0755 $(@D)/build/mister-runtime \
		$(TARGET_DIR)/usr/sbin/mister-runtime
endef

$(eval $(generic-package))
