PYTHON ?= python3
QUARTUS_ROOTDIR ?= /data/altera_pro/26.3
OUTPUT ?=
ARTIFACTS ?=
QEMU ?= qemu-aarch64-static
VENDOR_ARCHIVE ?=
CONTROL_PROOF_ARGS ?=
PLACEMENT ?= X2 Y7 X26 Y12
ROUTING ?= X1 Y6 X27 Y13

.PHONY: project build sim test proof control-proof
project build sim:
	@test -n "$(OUTPUT)" || { echo 'Set OUTPUT to a new directory'; exit 1; }
	$(PYTHON) build.py $@ --output "$(OUTPUT)" --quartus-root "$(QUARTUS_ROOTDIR)" --placement "$(PLACEMENT)" --routing "$(ROUTING)" --vendor-archive "$(VENDOR_ARCHIVE)"
test:
	$(PYTHON) -m unittest -v build_test
	cd target && go test ./...
proof:
	$(PYTHON) proof.py --artifacts "$(ARTIFACTS)" --output "$(OUTPUT)" --qemu "$(QEMU)"
control-proof:
	$(PYTHON) control_proof.py --output "$(OUTPUT)" $(CONTROL_PROOF_ARGS)
