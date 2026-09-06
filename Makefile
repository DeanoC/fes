.DEFAULT_GOAL := help
PROFILE ?= native-integration-dev
PYTHON ?= python3

.PHONY: help doctor build host image verify rebuild dev test check
help:
	@printf '%s\n' 'FES: start with AGENTS.md and docs/development.md' 'make check | doctor | build | host | image | verify | rebuild | dev | test' 'Default: native-integration-dev; historical: native-dev, native-source-dev' 'Source builds require QUARTUS_ROOTDIR; build does not deploy.'
doctor build host image verify rebuild dev:
	$(PYTHON) scripts/build.py $@ --profile "$(PROFILE)"
test:
	$(PYTHON) -m unittest discover -s tests -v
check:
	$(PYTHON) scripts/consistency.py
