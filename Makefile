.DEFAULT_GOAL := help
PROFILE ?= native-dev
PYTHON ?= python3

.PHONY: help doctor build host image verify rebuild test
help:
	@printf '%s\n' 'FES parent: pinned Linux host and native MiSTer image' 'make doctor | build | host | image | verify | rebuild | test' 'PROFILE=native-dev (the initial supported profile)' 'Outputs: out/native-dev/; build does not deploy.'
doctor build host image verify rebuild:
	$(PYTHON) scripts/build.py $@ --profile "$(PROFILE)"
test:
	$(PYTHON) -m unittest discover -s tests -v
