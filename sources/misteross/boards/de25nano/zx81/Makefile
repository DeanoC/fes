PYTHON ?= python3
QUARTUS_ROOTDIR ?=
VENDOR_ARCHIVE ?=
OUTPUT ?=

.PHONY: project build sim test
project build:
	$(PYTHON) build.py $@ --quartus-root "$(QUARTUS_ROOTDIR)" --vendor-archive "$(VENDOR_ARCHIVE)" $(if $(OUTPUT),--output "$(OUTPUT)")
sim:
	$(PYTHON) sim.py
test:
	$(PYTHON) -m unittest -v test_build
