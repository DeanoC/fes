.DEFAULT_GOAL := help
PROFILE ?= native-integration-dev
PYTHON ?= python3

ifneq ($(strip $(AGENT_CONFIG)),)
ifneq ($(strip $(filter-out media,$(MAKECMDGOALS))),)
$(error AGENT_CONFIG is supported only by make media)
endif
ifeq ($(strip $(MAKECMDGOALS)),)
$(error AGENT_CONFIG is supported only by make media)
endif
endif

.PHONY: help doctor build host image verify rebuild dev test check media verify-media rollback-media
help:
	@printf '%s\n' 'FES: start with AGENTS.md and docs/development.md' 'make check | doctor | build | host | image | verify | rebuild | dev | media | verify-media | rollback-media | test' 'Default: native-integration-dev; historical: native-dev, native-source-dev' 'Source builds require QUARTUS_ROOTDIR; build does not deploy.'
doctor build host image verify rebuild dev:
	$(PYTHON) scripts/build.py $@ --profile "$(PROFILE)"
test:
	$(PYTHON) -m unittest discover -s tests -v
check:
	$(PYTHON) scripts/consistency.py

media:
	$(PYTHON) scripts/media.py build --profile "$(PROFILE)" $(if $(AGENT_CONFIG),--agent-config "$(AGENT_CONFIG)")
verify-media:
	$(PYTHON) scripts/media.py verify --profile "$(PROFILE)"

rollback-media:
	$(if $(strip $(GENERATION)),,$(error rollback-media requires GENERATION=<image-sha256>/<evidence-sha256>))
	$(PYTHON) scripts/media.py rollback --profile "$(PROFILE)" --generation "$(GENERATION)"
