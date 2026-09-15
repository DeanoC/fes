.DEFAULT_GOAL := help
PROFILE ?= native-integration-dev
PYTHON ?= python3
RELEASE_VERSION ?= 0.2.0-dev.1

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

.PHONY: help doctor build host image verify rebuild dev test check target-acceptance media verify-media rollback-media
TARGET_ACCEPTANCE_SELECTION_DIR ?= out/$(PROFILE)/development
TARGET_ACCEPTANCE_ARGS ?=
help:
	@printf '%s\n' 'FES: start with AGENTS.md and docs/development.md' 'make check | doctor | build | host | image | verify | rebuild | dev | target-acceptance | media | verify-media | rollback-media | test' 'Default: native-integration-dev; FES image lanes are package-only' 'make media auto-embeds the private host token; use FES_UNPROVISIONED=1 for CI-only media.' 'Quartus is an explicit oracle/check for unsupported systems; build does not deploy.'
doctor build host image verify rebuild dev:
	$(PYTHON) scripts/build.py $@ --profile "$(PROFILE)"
test:
	$(PYTHON) -m unittest discover -s tests -v
	$(MAKE) -C image test FOGCAST_DIR="$(abspath sources/FogCast)"
check:
	$(PYTHON) scripts/consistency.py
target-acceptance:
	$(PYTHON) scripts/target_acceptance.py --selection-dir "$(TARGET_ACCEPTANCE_SELECTION_DIR)" $(TARGET_ACCEPTANCE_ARGS)

media:
	$(PYTHON) scripts/media.py build --profile "$(PROFILE)" $(if $(AGENT_CONFIG),--agent-config "$(AGENT_CONFIG)",$(if $(filter 1 true yes on,$(FES_UNPROVISIONED)),--unprovisioned,--auto-agent-config))
verify-media:
	$(PYTHON) scripts/media.py verify --profile "$(PROFILE)"

rollback-media:
	$(if $(strip $(GENERATION)),,$(error rollback-media requires GENERATION=<image-sha256>/<evidence-sha256>))
	$(PYTHON) scripts/media.py rollback --profile "$(PROFILE)" --generation "$(GENERATION)"

.PHONY: release bootstrap appliance-media verify-appliance-media
release:
	$(PYTHON) scripts/appliance.py release --profile "$(PROFILE)" --version "$(RELEASE_VERSION)"
bootstrap:
	$(if $(strip $(RELEASE)),,$(error bootstrap requires RELEASE=/absolute/path/to/release-directory))
	$(PYTHON) scripts/appliance.py bootstrap --profile "$(PROFILE)" --release "$(RELEASE)"

appliance-media verify-appliance-media:
	$(if $(strip $(RELEASE)),,$(error appliance media requires RELEASE=/absolute/path/to/release-directory))
	$(if $(strip $(BOOTSTRAP)),,$(error appliance media requires BOOTSTRAP=/absolute/path/to/bootstrap-directory))
	$(if $(strip $(OUTPUT)),,$(error appliance media requires OUTPUT=/absolute/path/to/card-directory))
	$(PYTHON) scripts/appliance_media.py $(if $(filter appliance-media,$@),build,verify) --profile "$(PROFILE)" --release "$(RELEASE)" --bootstrap "$(BOOTSTRAP)" --output "$(OUTPUT)"
