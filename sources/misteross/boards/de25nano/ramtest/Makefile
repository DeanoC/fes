PYTHON ?= python3
QUARTUS_ROOTDIR ?=
VENDOR_ARCHIVE ?=
OUTPUT ?=
PROJECT ?=

.PHONY: project generate build sim sim-interconnect test
project generate build:
	$(PYTHON) build.py $@ --quartus-root "$(QUARTUS_ROOTDIR)" --vendor-archive "$(VENDOR_ARCHIVE)" $(if $(OUTPUT),--output "$(OUTPUT)")
sim:
	$(PYTHON) sim.py
sim-interconnect:
	$(PYTHON) interconnect_sim.py --project "$(PROJECT)" --output "$(OUTPUT)"
test:
	$(PYTHON) -m unittest -v test_build test_jtag test_interconnect
