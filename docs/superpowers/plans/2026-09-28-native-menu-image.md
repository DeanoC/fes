# Native menu image integration

The approved native-menu design has completed diagnostic and kit-shell stages. This slice selects the menu through FES's sealed package recipe, installs one authenticated menu selection in the native image, and configures runtime and kit UI at boot. The U-Boot splash remains the boot and failure fallback. `fes.menu` is an image service, not a library game.

1. Adapt the existing menu package producer to the FES format-2 canonical recipe API and register the recipe. Include its authenticated input set and prove canonical prebuild record agrees with the sealed package.
2. Add `fes.menu` to the development image profile and every closed image package mapping. Install its selection and make runtime startup consume the selected package ID. Start the kit UI in menu-display mode. Fail closed for absent or malformed selections.
3. Add focused producer, image and startup regression checks, then run component tests and parent consistency. Commit clean source before package synthesis because FES builders require immutable source provenance.
4. Build and verify the exact image and, when the designated kit lease is available, validate boot identity and repeated menu→game→Stop→menu handoffs. Record physical evidence separately from host build evidence.
