PYTHON ?= python3
MODE ?= idle
QUARTUS_ROOTDIR ?=
VENDOR_ARCHIVE ?=
OUTPUT ?=

.PHONY: project generate build idle menu-pattern sim test
project generate build:
	$(PYTHON) build.py $@ --mode "$(MODE)" --quartus-root "$(QUARTUS_ROOTDIR)" --vendor-archive "$(VENDOR_ARCHIVE)" $(if $(OUTPUT),--output "$(OUTPUT)")
idle menu-pattern:
	$(MAKE) build MODE=$@
sim:
	$(PYTHON) sim.py
test:
	$(PYTHON) -m unittest -v test_build
