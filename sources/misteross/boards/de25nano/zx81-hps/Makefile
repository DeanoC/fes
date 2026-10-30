PYTHON ?= python3
QUARTUS_ROOTDIR ?=
VENDOR_ARCHIVE ?=
OUTPUT ?=
CLIENT_PROOF_ARGS ?=

.PHONY: project generate build sim test client-proof video-sim
project generate build:
	$(PYTHON) build.py $@ --quartus-root "$(QUARTUS_ROOTDIR)" --vendor-archive "$(VENDOR_ARCHIVE)" $(if $(OUTPUT),--output "$(OUTPUT)")
sim:
	$(PYTHON) video_sim.py
	$(PYTHON) sim.py
test:
	$(PYTHON) -m unittest -v test_build test_client test_client_proof
client-proof:
	$(PYTHON) client_proof.py $(if $(OUTPUT),--output "$(OUTPUT)") $(CLIENT_PROOF_ARGS)

video-sim:
	$(PYTHON) video_sim.py $(if $(OUTPUT),--output "$(OUTPUT)")
