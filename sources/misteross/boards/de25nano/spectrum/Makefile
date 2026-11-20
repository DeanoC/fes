PYTHON ?= python3
QUARTUS_ROOTDIR ?=
VENDOR_ARCHIVE ?=
OUTPUT ?=
MODEL ?= 48

.PHONY: project generate build sim test
project generate build:
	$(PYTHON) build.py $@ --quartus-root "$(QUARTUS_ROOTDIR)" --vendor-archive "$(VENDOR_ARCHIVE)" --output "$(OUTPUT)" --model $(MODEL)
sim:
	$(PYTHON) sim.py
test:
	$(PYTHON) -m unittest -v test_build
