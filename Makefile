.DEFAULT_GOAL := help
PROFILE ?= native-integration-dev
PYTHON ?= python3
RELEASE_VERSION ?= 0.2.0-dev.1
IMAGE_PASSES ?= 1
ifneq ($(origin FES_TARGET_IMAGE_SHARED_CACHE),undefined)
export FES_TARGET_IMAGE_SHARED_CACHE
endif

ifneq ($(strip $(AGENT_CONFIG)),)
ifneq ($(strip $(filter-out media,$(MAKECMDGOALS))),)
$(error AGENT_CONFIG is supported only by make media)
endif
ifeq ($(strip $(MAKECMDGOALS)),)
$(error AGENT_CONFIG is supported only by make media)
endif
endif

ifneq ($(strip $(FES_UNPROVISIONED)),)
ifneq ($(strip $(filter-out media,$(MAKECMDGOALS))),)
$(error FES_UNPROVISIONED is supported only by make media)
endif
endif

.PHONY: help doctor build host image verify rebuild dev test platform-provision platform-test check target-acceptance media verify-media rollback-media
TARGET_ACCEPTANCE_SELECTION_DIR ?= out/$(PROFILE)/development
TARGET_ACCEPTANCE_ARGS ?=
PACKAGE_ACCEPTANCE_ARGS ?=
PACKAGE_ACCEPTANCE_ISOLATED_ARGS ?=
CORE_CATALOG_ARGS ?=
CORE_DEV_ARGS ?=
CORE_DEV_ACCEPT_ARGS ?=
TEST_CHANGED_ARGS ?= --base origin/main
DE25_LINUX_ARGS ?=
help:
	@printf '%s\n' 'FES: start with AGENTS.md and docs/development.md' 'make source-status | check | doctor | build | host | image | verify | rebuild | dev | platform-provision | platform-test | target-acceptance | media | verify-media | rollback-media | test' 'Default: native-integration-dev, the DE10-Nano package-only image' 'Appliance bootstrap builds FES platform/ against the selected FogCast appliance module.' 'make media auto-embeds the private host token; use FES_UNPROVISIONED=1 for CI-only media.' 'Quartus is an explicit oracle/check for unsupported systems; build does not deploy.'
	@printf '%s\n' 'make package-acceptance PACKAGE_ACCEPTANCE_ARGS="--help": one sealed package, explicit hardware opt-in, no image rebuild.'
	@printf '%s\n' 'make package-acceptance-isolated PACKAGE_ACCEPTANCE_ISOLATED_ARGS="--help": private host catalog and restart diagnostic; explicit hardware opt-in.'
	@printf '%s\n' 'make core-dev CORE_DEV_ARGS="--help": prepare one HIP package without building an image.' 'make core-dev-accept CORE_DEV_ACCEPT_ARGS="--help": explicit isolated acceptance of a frozen candidate.'
	@printf '%s\n' 'make kit-sdk: build and publish the current image Buildroot SDK into the toolchain cache, without an image or core build.'
	@printf '%s\n' 'make kit-software KIT_SOFTWARE_BASE=/absolute/linux.img KIT_SOFTWARE_BASE_SHA256=SHA256 KIT_SOFTWARE_VERSION=VERSION: build compatible kit userspace without an OS rebuild.'
	@printf '%s\n' 'make fetch-de25-linux | de25-linux | verify-de25-linux | smoke-de25-linux: separate headless DE25-Nano AArch64 Linux diagnostic; see docs/de25-linux.md.'
	@printf '%s\n' 'make smoke-de25-runtime DE25_RUNTIME_ARGS="--help": cross-build AArch64 runtime/agent and exercise software under QEMU without a board.'
IMAGE_ARTIFACTS ?=
doctor build host image verify rebuild dev:
	IMAGE_PASSES="$(IMAGE_PASSES)" $(PYTHON) scripts/build.py $@ --profile "$(PROFILE)" $(if $(IMAGE_ARTIFACTS),--artifacts "$(IMAGE_ARTIFACTS)")

.PHONY: de25-linux fetch-de25-linux verify-de25-linux smoke-de25-linux
de25-linux:
	$(PYTHON) image/de25-nano/build.py build $(DE25_LINUX_ARGS)
fetch-de25-linux:
	$(PYTHON) image/de25-nano/build.py fetch $(DE25_LINUX_ARGS)
verify-de25-linux:
	$(PYTHON) image/de25-nano/build.py verify $(DE25_LINUX_ARGS)
smoke-de25-linux:
	$(PYTHON) image/de25-nano/build.py smoke $(DE25_LINUX_ARGS)

.PHONY: smoke-de25-runtime
smoke-de25-runtime:
	$(PYTHON) scripts/de25_runtime.py $(DE25_RUNTIME_ARGS)

platform-provision:
	$(PYTHON) scripts/platform.py provision --fogcast "$(abspath sources/FogCast)"
platform-test:
	$(PYTHON) scripts/platform.py test --fogcast "$(abspath sources/FogCast)"
test:
	$(MAKE) platform-provision
	$(MAKE) media-test-provision
	$(PYTHON) -m unittest discover -s tests -v
	$(MAKE) platform-test
	$(MAKE) -C image test FOGCAST_DIR="$(abspath sources/FogCast)"
check:
	$(PYTHON) scripts/consistency.py
.PHONY: deadcode
deadcode:
	$(PYTHON) scripts/check_deadcode.py
.PHONY: media-test-provision
media-test-provision:
	$(PYTHON) -c 'import os; from pathlib import Path; from scripts.media_container import ensure_media_container; from scripts.media_inputs import MediaLock; ensure_media_container(Path("."), os.environ.get("CONTAINER_RUNTIME", "docker"), MediaLock.load(Path("boot-media.lock.toml")))'
.PHONY: test-changed
test-changed:
	$(PYTHON) scripts/test_changed.py $(TEST_CHANGED_ARGS)
.PHONY: dev-snapshot
dev-snapshot:
	$(PYTHON) scripts/dev_snapshot.py
target-acceptance:
	$(PYTHON) scripts/target_acceptance.py --selection-dir "$(TARGET_ACCEPTANCE_SELECTION_DIR)" $(TARGET_ACCEPTANCE_ARGS)

.PHONY: package-acceptance
.PHONY: core-dev core-dev-accept
core-dev:
	$(PYTHON) scripts/core_dev.py $(CORE_DEV_ARGS)
core-dev-accept:
	$(PYTHON) scripts/core_dev_accept.py $(CORE_DEV_ACCEPT_ARGS)

package-acceptance:
	$(PYTHON) scripts/package_acceptance.py $(PACKAGE_ACCEPTANCE_ARGS)

.PHONY: package-acceptance-isolated
package-acceptance-isolated:
	$(PYTHON) scripts/package_acceptance_isolated.py $(PACKAGE_ACCEPTANCE_ISOLATED_ARGS)

media:
	$(PYTHON) scripts/media.py build --profile "$(PROFILE)" $(if $(AGENT_CONFIG),--agent-config "$(AGENT_CONFIG)",$(if $(filter 1 true yes on,$(FES_UNPROVISIONED)),--unprovisioned,--auto-agent-config))
verify-media:
	$(PYTHON) scripts/media.py verify --profile "$(PROFILE)"

rollback-media:
	$(if $(strip $(GENERATION)),,$(error rollback-media requires GENERATION=<image-sha256>/<evidence-sha256>))
	$(PYTHON) scripts/media.py rollback --profile "$(PROFILE)" --generation "$(GENERATION)"

.PHONY: release bootstrap appliance-media verify-appliance-media
release:
	$(PYTHON) scripts/appliance.py release --profile "$(PROFILE)" --version "$(RELEASE_VERSION)" --image-passes "$(IMAGE_PASSES)"
bootstrap: platform-provision
	$(if $(strip $(RELEASE)),,$(error bootstrap requires RELEASE=/absolute/path/to/release-directory))
	$(PYTHON) scripts/appliance.py bootstrap --profile "$(PROFILE)" --release "$(RELEASE)"

appliance-media verify-appliance-media:
	$(if $(strip $(RELEASE)),,$(error appliance media requires RELEASE=/absolute/path/to/release-directory))
	$(if $(strip $(BOOTSTRAP)),,$(error appliance media requires BOOTSTRAP=/absolute/path/to/bootstrap-directory))
	$(if $(strip $(OUTPUT)),,$(error appliance media requires OUTPUT=/absolute/path/to/card-directory))
	$(PYTHON) scripts/appliance_media.py $(if $(filter appliance-media,$@),build,verify) --profile "$(PROFILE)" --release "$(RELEASE)" --bootstrap "$(BOOTSTRAP)" --output "$(OUTPUT)"

.PHONY: source-status
SOURCE_STATUS_ARGS ?=
source-status:
	@$(PYTHON) scripts/source_status.py $(SOURCE_STATUS_ARGS)

.PHONY: generate check-generated
generate:
	@$(PYTHON) scripts/generate.py --write
check-generated:
	@$(PYTHON) scripts/generate.py --check

.PHONY: status
STATUS_ARGS ?=
status:
	@$(PYTHON) scripts/status.py $(STATUS_ARGS)

.PHONY: core-catalog
core-catalog:
	$(PYTHON) scripts/core_catalog.py $(CORE_CATALOG_ARGS)

.PHONY: kit-sdk
kit-sdk:
	@set -eu; \
	  TARGET_IMAGE_CONTAINER_RUNTIME="$(or $(TARGET_IMAGE_CONTAINER_RUNTIME),$(CONTAINER_RUNTIME),docker)"; \
	  export TARGET_IMAGE_CONTAINER_RUNTIME; \
	  output_volume="fogcast-kit-sdk-$$(python3 -c 'import uuid; print(uuid.uuid4().hex)')"; \
	  cleanup() { "$$TARGET_IMAGE_CONTAINER_RUNTIME" volume rm "$$output_volume" >/dev/null 2>&1 || :; }; \
	  cleanup_once() { trap - EXIT INT TERM; cleanup; }; \
	  trap cleanup_once EXIT; \
	  trap 'exit 130' INT; \
	  trap 'exit 143' TERM; \
	  TARGET_IMAGE_OUTPUT_VOLUME="$$output_volume" \
	    sh image/scripts/build-target-image.sh --ensure-kit-sdk

.PHONY: kit-software
KIT_SOFTWARE_OUTPUT ?= out/kit-software/$(KIT_SOFTWARE_VERSION)
kit-software:
	$(if $(strip $(KIT_SOFTWARE_BASE)),,$(error kit-software requires KIT_SOFTWARE_BASE=/absolute/linux.img))
	$(if $(strip $(KIT_SOFTWARE_BASE_SHA256)),,$(error kit-software requires KIT_SOFTWARE_BASE_SHA256=<reviewed image SHA-256>))
	$(if $(strip $(KIT_SOFTWARE_VERSION)),,$(error kit-software requires KIT_SOFTWARE_VERSION=<release version>))
	$(PYTHON) scripts/kit_software.py --base "$(KIT_SOFTWARE_BASE)" --base-sha256 "$(KIT_SOFTWARE_BASE_SHA256)" --version "$(KIT_SOFTWARE_VERSION)" --output "$(KIT_SOFTWARE_OUTPUT)"
