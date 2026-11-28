package misterruntime

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/DeanoC/FogCast/protocol"
)

func decodeNegotiationResponse(line []byte) (Protocol2Response, error) {
	return decodeProtocol2Response(line)
}

func decodeProtocol2Response(line []byte) (Protocol2Response, error) {
	if err := rejectDuplicateJSONNames(line); err != nil {
		return Protocol2Response{}, errInvalidRuntimeResponse
	}
	if err := validateProtocol2Shape(line); err != nil {
		return Protocol2Response{}, errInvalidRuntimeResponse
	}
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	var response Protocol2Response
	if err := decoder.Decode(&response); err != nil {
		return Protocol2Response{}, errInvalidRuntimeResponse
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Protocol2Response{}, errInvalidRuntimeResponse
	}
	if response.ActivePackage != nil && response.ActivePackage.ROMLink != nil && response.ActivePackage.ROMLink.Role == "" && response.ActivePackage.Descriptor.ROM != nil {
		response.ActivePackage.ROMLink.Role = response.ActivePackage.Descriptor.ROM.Role
	}
	if !validProtocol2Response(response) {
		return Protocol2Response{}, errInvalidRuntimeResponse
	}
	return response, nil
}

func validateProtocol2Shape(line []byte) error {
	var root json.RawMessage = line
	object, err := exactRawObject(root, []string{"protocol", "ok", "state", "execution", "system", "core", "error", "version", "capabilities", "active_package", "runtime_instance", "generation", "inspected_package"}, []string{"core_data", "menu_display", "media_data"})
	if err != nil {
		return err
	}
	if err := requireRawKinds(object, map[string]rawKind{
		"protocol": rawUnsigned, "ok": rawBoolean, "state": rawString,
		"execution": rawString, "system": rawNullableString,
		"core": rawNullableString, "error": rawNullableObject,
		"version": rawString, "capabilities": rawObject,
		"active_package": rawNullableObject, "runtime_instance": rawNullableString, "generation": rawNullableUnsigned,
		"inspected_package": rawNullableObject,
	}); err != nil {
		return err
	}
	if data, ok := object["core_data"]; ok && !isNull(data) {
		if err := validateCoreDataShape(data); err != nil {
			return err
		}
	}
	if media, ok := object["media_data"]; ok {
		record, err := exactRawObject(media, []string{"state", "revision", "bytes", "changed"}, nil)
		if err != nil {
			return err
		}
		if err := requireRawKinds(record, map[string]rawKind{"state": rawString, "revision": rawString, "bytes": rawUnsigned, "changed": rawBoolean}); err != nil {
			return err
		}
	}
	if menu, ok := object["menu_display"]; ok {
		if err := validateMenuDisplayShape(menu); err != nil {
			return err
		}
	}
	if !isNull(object["error"]) {
		if err := validateErrorShape(object["error"]); err != nil {
			return err
		}
	}
	capabilities, err := exactRawObject(object["capabilities"], []string{"programming_profiles", "abis", "active_interfaces"}, []string{"media_stream", "rom_linking", "media_units"})
	if err != nil {
		return err
	}
	if err := requireRawKinds(capabilities, map[string]rawKind{
		"programming_profiles": rawArray, "abis": rawArray,
		"active_interfaces": rawArray,
	}); err != nil {
		return err
	}
	if raw, ok := capabilities["rom_linking"]; ok {
		if err := requireRawKind(raw, rawUnsigned); err != nil {
			return err
		}
	}
	if raw, ok := capabilities["media_stream"]; ok {
		stream, err := exactRawObject(raw, []string{"interface", "min_bytes", "max_bytes", "chunk_bytes"}, nil)
		if err != nil {
			return err
		}
		if err := requireRawKinds(stream, map[string]rawKind{"interface": rawObject, "min_bytes": rawUnsigned, "max_bytes": rawUnsigned, "chunk_bytes": rawUnsigned}); err != nil {
			return err
		}
		if err := validateSupportedInterfaceShape(stream["interface"]); err != nil {
			return err
		}
	}
	if raw, ok := capabilities["media_units"]; ok {
		if err := requireRawKind(raw, rawArray); err != nil {
			return err
		}
		if err := eachRaw(raw, func(item json.RawMessage) error {
			unit, err := exactRawObject(item, []string{"unit", "interface", "min_bytes", "max_bytes", "chunk_bytes", "state"}, []string{"persistence", "saveable"})
			if err != nil {
				return err
			}
			if err := requireRawKinds(unit, map[string]rawKind{"unit": rawUnsigned, "interface": rawObject, "min_bytes": rawUnsigned, "max_bytes": rawUnsigned, "chunk_bytes": rawUnsigned, "state": rawString}); err != nil {
				return err
			}
			if raw, ok := unit["persistence"]; ok {
				binding, err := exactRawObject(raw, []string{"mode", "game_id", "base_media_id", "revision"}, nil)
				if err != nil {
					return err
				}
				if err = requireRawKinds(binding, map[string]rawKind{"mode": rawString, "game_id": rawString, "base_media_id": rawString, "revision": rawString}); err != nil {
					return err
				}
			}
			if raw, ok := unit["saveable"]; ok {
				if err := requireRawKind(raw, rawBoolean); err != nil {
					return err
				}
			}
			return validateSupportedInterfaceShape(unit["interface"])
		}); err != nil {
			return err
		}
	}
	if err := eachRaw(capabilities["programming_profiles"], func(raw json.RawMessage) error {
		return requireRawKind(raw, rawString)
	}); err != nil {
		return err
	}
	if err := eachRaw(capabilities["abis"], func(raw json.RawMessage) error {
		abi, err := exactRawObject(raw, []string{"id", "major", "minor", "interfaces"}, nil)
		if err != nil {
			return err
		}
		if err := requireRawKinds(abi, map[string]rawKind{
			"id": rawString, "major": rawUnsigned, "minor": rawUnsigned,
			"interfaces": rawArray,
		}); err != nil {
			return err
		}
		return eachRaw(abi["interfaces"], validateSupportedInterfaceShape)
	}); err != nil {
		return err
	}
	if err := eachRaw(capabilities["active_interfaces"], validateSupportedInterfaceShape); err != nil {
		return err
	}
	if !isNull(object["active_package"]) {
		active, err := exactRawObject(object["active_package"], []string{"package_id", "descriptor", "observed"}, []string{"persistence_mode", "composition", "rom_link", "rom_links"})
		if err != nil {
			return err
		}
		if err := requireRawKinds(active, map[string]rawKind{
			"package_id": rawString, "descriptor": rawObject,
			"observed": rawObject,
		}); err != nil {
			return err
		}
		if raw, ok := active["rom_link"]; ok {
			fields, err := exactRawObject(raw, []string{"rom_id", "map_sha256", "source_sha256", "source_size", "programmed_sha256", "programmed_size"}, []string{"role"})
			if err != nil {
				return err
			}
			if err := requireRawKinds(fields, map[string]rawKind{"rom_id": rawString, "map_sha256": rawString, "source_sha256": rawString, "source_size": rawUnsigned, "programmed_sha256": rawString, "programmed_size": rawUnsigned}); err != nil {
				return err
			}
			if _, ok := fields["role"]; ok {
				if err := requireRawKinds(fields, map[string]rawKind{"role": rawString}); err != nil {
					return err
				}
			}
		}
		if raw, ok := active["rom_links"]; ok {
			fields, err := exactRawObject(raw, []string{"sources", "map_sha256", "programmed_sha256", "programmed_size"}, nil)
			if err != nil {
				return err
			}
			if err := requireRawKinds(fields, map[string]rawKind{"sources": rawArray, "map_sha256": rawString, "programmed_sha256": rawString, "programmed_size": rawUnsigned}); err != nil {
				return err
			}
			if err := eachRaw(fields["sources"], func(item json.RawMessage) error {
				source, err := exactRawObject(item, []string{"id", "role", "source_sha256", "source_size"}, nil)
				if err != nil {
					return err
				}
				return requireRawKinds(source, map[string]rawKind{"id": rawString, "role": rawString, "source_sha256": rawString, "source_size": rawUnsigned})
			}); err != nil {
				return err
			}
		}
		if value, ok := active["composition"]; ok && !isNull(value) {
			if err := validateCompositionShape(value); err != nil {
				return err
			}
		}
		if mode, ok := active["persistence_mode"]; ok {
			var value string
			if json.Unmarshal(mode, &value) != nil || (value != "persistent" && value != "volatile") {
				return errInvalidRuntimeResponse
			}
		}
		if err := validateDescriptorShape(active["descriptor"]); err != nil {
			return err
		}
		observed, err := exactRawObject(active["observed"], []string{"abi", "build_id"}, nil)
		if err != nil {
			return err
		}
		if err := requireRawKinds(observed, map[string]rawKind{
			"abi": rawNullableObject, "build_id": rawNullableString,
		}); err != nil {
			return err
		}
		if !isNull(observed["abi"]) {
			abi, err := exactRawObject(observed["abi"], []string{"id", "major", "minor"}, nil)
			if err != nil {
				return err
			}
			if err := requireRawKinds(abi, map[string]rawKind{
				"id": rawString, "major": rawUnsigned, "minor": rawUnsigned,
			}); err != nil {
				return err
			}
		}
	}
	if !isNull(object["inspected_package"]) {
		inspection, err := exactRawObject(object["inspected_package"], []string{"package_id", "descriptor", "compatible", "compatibility_error"}, []string{"persistence_layout"})
		if err != nil {
			return err
		}
		if err := requireRawKinds(inspection, map[string]rawKind{
			"package_id": rawString, "descriptor": rawObject,
			"compatible": rawBoolean, "compatibility_error": rawNullableObject,
		}); err != nil {
			return err
		}
		if layout, ok := inspection["persistence_layout"]; ok && !isNull(layout) {
			if err := validateSupportedInterfaceShape(layout); err != nil {
				return err
			}
		}
		if err := validateDescriptorShape(inspection["descriptor"]); err != nil {
			return err
		}
		if !isNull(inspection["compatibility_error"]) {
			if err := validateErrorShape(inspection["compatibility_error"]); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateMenuDisplayShape(raw json.RawMessage) error {
	menu, err := exactRawObject(raw, []string{"available", "package_id", "generation", "width", "height", "stride", "byte_count", "slot_bytes", "slots", "format", "displayed_sequence", "underflows", "error"}, []string{"session", "core_generation", "focus", "picture", "frame_mapping", "live"})
	if err != nil {
		return err
	}
	if err := requireRawKinds(menu, map[string]rawKind{
		"available": rawBoolean, "package_id": rawNullableString,
		"generation": rawUnsigned, "width": rawUnsigned, "height": rawUnsigned,
		"stride": rawUnsigned, "byte_count": rawUnsigned, "slot_bytes": rawUnsigned,
		"slots": rawUnsigned, "format": rawString, "displayed_sequence": rawUnsigned,
		"underflows": rawUnsigned, "error": rawNullableObject,
	}); err != nil {
		return err
	}
	if mapping, present := menu["frame_mapping"]; present {
		if err := requireRawKind(mapping, rawBoolean); err != nil {
			return err
		}
	}
	if session, present := menu["session"]; present {
		if !bytes.Equal(bytes.TrimSpace(session), []byte("true")) || requireRawKinds(menu, map[string]rawKind{"core_generation": rawUnsigned, "focus": rawString}) != nil {
			return errInvalidRuntimeResponse
		}
		var generation uint64
		var focus protocol.SessionFocus
		if json.Unmarshal(menu["core_generation"], &generation) != nil || generation == 0 || isNull(menu["package_id"]) ||
			json.Unmarshal(menu["focus"], &focus) != nil || focus != protocol.SessionFocusMachine && focus != protocol.SessionFocusLauncher {
			return errInvalidRuntimeResponse
		}
	} else {
		for _, field := range []string{"core_generation", "focus", "picture"} {
			if _, present := menu[field]; present {
				return errInvalidRuntimeResponse
			}
		}
	}
	if raw, present := menu["picture"]; present {
		// A slice, not [4]uint16: decoding into an array silently drops
		// surplus elements, and the rectangle has exactly four.
		var picture []uint16
		var width, height uint64
		if json.Unmarshal(raw, &picture) != nil || len(picture) != 4 || json.Unmarshal(menu["width"], &width) != nil ||
			json.Unmarshal(menu["height"], &height) != nil || picture[2] == 0 || picture[3] == 0 ||
			uint64(picture[0])+uint64(picture[2]) > width || uint64(picture[1])+uint64(picture[3]) > height {
			return errInvalidRuntimeResponse
		}
	}
	if raw, present := menu["live"]; present {
		// recovery_abandoned is optional: a runtime that measures the display
		// but never says it gave up is read as still willing to recover.
		// scanout_frame is optional for the same reason: a runtime older than
		// the verdict publishes none, and must keep validating exactly as it
		// does today rather than being refused for a member it never had. Any
		// string passes the shape check; a reader acts only on "mismatched",
		// so a name this client does not know costs a probe interval instead
		// of manufacturing a loss.
		live, err := exactRawObject(raw, []string{"enabled", "faulted", "quiesced", "displayed_sequence", "underflows_total"}, []string{"recovery_abandoned", "scanout_frame", "transmitter"})
		if err != nil {
			return err
		}
		if err := requireRawKinds(live, map[string]rawKind{
			"enabled": rawBoolean, "faulted": rawBoolean, "quiesced": rawBoolean,
			"displayed_sequence": rawUnsigned, "underflows_total": rawUnsigned,
			"recovery_abandoned": rawBoolean,
		}); err != nil {
			return err
		}
		// Checked only when it is there: requireRawKinds also requires the
		// member, and a runtime older than the verdict sends none.
		if verdict, present := live["scanout_frame"]; present {
			if err := requireRawKind(verdict, rawString); err != nil {
				return err
			}
		}
		// The transmitter reads arrive together or not at all, so they are
		// their own object rather than four optional members that a re-encode
		// could split.
		if raw, present := live["transmitter"]; present {
			reads, err := exactRawObject(raw, []string{"hot_plug", "rx_sense", "pll_lock", "vic"}, nil)
			if err != nil {
				return err
			}
			if err := requireRawKinds(reads, map[string]rawKind{
				"hot_plug": rawBoolean, "rx_sense": rawBoolean, "pll_lock": rawBoolean, "vic": rawUnsigned,
			}); err != nil {
				return err
			}
		}
	}
	if !isNull(menu["error"]) {
		return validateErrorShape(menu["error"])
	}
	return nil
}

// validateCompositionShape accepts the single-socket v1 tuple or the
// multi-slot v2 tuple, whose expansions array replaces expansion_id.
func validateCompositionShape(value json.RawMessage) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(value, &probe); err != nil {
		return err
	}
	if _, static := probe["base_sha256"]; static {
		tuple, err := exactRawObject(value, []string{"composition_id", "package_id", "base_sha256", "base_size", "persona_sha256", "persona_size", "control_build_id", "hardware_id"}, []string{"expansion_id"})
		if err != nil {
			return err
		}
		kinds := map[string]rawKind{"composition_id": rawString, "package_id": rawString, "base_sha256": rawString, "base_size": rawUnsigned, "persona_sha256": rawString, "persona_size": rawUnsigned, "control_build_id": rawString, "hardware_id": rawUnsigned}
		if _, ok := tuple["expansion_id"]; ok {
			kinds["expansion_id"] = rawString
		}
		return requireRawKinds(tuple, kinds)
	} else if _, parts := probe["parts"]; parts {
		tuple, err := exactRawObject(value, []string{"composition_id", "package_id", "layout", "parts", "shell_sha256", "payload_sha256", "payload_size"}, nil)
		if err != nil {
			return err
		}
		if err := requireRawKinds(tuple, map[string]rawKind{"composition_id": rawString, "package_id": rawString, "layout": rawString, "parts": rawArray, "shell_sha256": rawString, "payload_sha256": rawString, "payload_size": rawUnsigned}); err != nil {
			return err
		}
		return eachRaw(tuple["parts"], func(item json.RawMessage) error {
			fields, err := exactRawObject(item, []string{"role", "part_id"}, nil)
			if err != nil {
				return err
			}
			return requireRawKinds(fields, map[string]rawKind{"role": rawString, "part_id": rawString})
		})
	} else if _, slots := probe["expansions"]; slots {
		tuple, err := exactRawObject(value, []string{"composition_id", "package_id", "expansions", "shell_sha256", "payload_sha256", "payload_size"}, nil)
		if err != nil {
			return err
		}
		if err := requireRawKinds(tuple, map[string]rawKind{"composition_id": rawString, "package_id": rawString, "expansions": rawArray, "shell_sha256": rawString, "payload_sha256": rawString, "payload_size": rawUnsigned}); err != nil {
			return err
		}
		return eachRaw(tuple["expansions"], func(item json.RawMessage) error {
			fields, err := exactRawObject(item, []string{"slot", "expansion_id"}, nil)
			if err != nil {
				return err
			}
			return requireRawKinds(fields, map[string]rawKind{"slot": rawUnsigned, "expansion_id": rawString})
		})
	}
	tuple, err := exactRawObject(value, []string{"composition_id", "package_id", "expansion_id", "shell_sha256", "payload_sha256", "payload_size"}, nil)
	if err != nil {
		return err
	}
	return requireRawKinds(tuple, map[string]rawKind{"composition_id": rawString, "package_id": rawString, "expansion_id": rawString, "shell_sha256": rawString, "payload_sha256": rawString, "payload_size": rawUnsigned})
}

func validateDescriptorShape(raw json.RawMessage) error {
	descriptor, err := exactRawObject(raw, []string{"format", "core", "target", "payload", "abi", "interfaces", "build"}, []string{"rom", "roms", "rom_map", "static_expansion"})
	if err != nil {
		return err
	}
	if err := requireRawKinds(descriptor, map[string]rawKind{
		"format": rawUnsigned, "core": rawObject, "target": rawObject,
		"payload": rawObject, "abi": rawObject, "interfaces": rawArray,
		"build": rawObject,
	}); err != nil {
		return err
	}
	var format uint64
	if err := json.Unmarshal(descriptor["format"], &format); err != nil {
		return errInvalidRuntimeResponse
	}
	static, hasStatic := descriptor["static_expansion"]
	if (format == 5) != hasStatic {
		return errInvalidRuntimeResponse
	}
	if hasStatic {
		fields, err := exactRawObject(static, []string{"file", "size", "sha256", "control_build_id", "hardware_id", "root_qdb_sha256", "interface_sha256", "clock_reset_sha256", "pof0_hash"}, nil)
		if err != nil {
			return err
		}
		if err := requireRawKinds(fields, map[string]rawKind{"file": rawString, "size": rawUnsigned, "sha256": rawString, "control_build_id": rawString, "hardware_id": rawUnsigned, "root_qdb_sha256": rawString, "interface_sha256": rawString, "clock_reset_sha256": rawString, "pof0_hash": rawString}); err != nil {
			return err
		}
	}
	rom, hasROM := descriptor["rom"]
	if (format == 3) != hasROM {
		return errInvalidRuntimeResponse
	}
	roms, hasROMs := descriptor["roms"]
	romMap, hasROMMap := descriptor["rom_map"]
	if (format == 4) != hasROMs || (format == 4) != hasROMMap {
		return errInvalidRuntimeResponse
	}
	if hasROMs {
		if err := eachRaw(roms, func(item json.RawMessage) error {
			fields, err := exactRawObject(item, []string{"id", "role", "source_size", "source_offset"}, nil)
			if err != nil {
				return err
			}
			return requireRawKinds(fields, map[string]rawKind{"id": rawString, "role": rawString, "source_size": rawUnsigned, "source_offset": rawUnsigned})
		}); err != nil {
			return err
		}
		fields, err := exactRawObject(romMap, []string{"file", "size", "sha256"}, nil)
		if err != nil {
			return err
		}
		if err := requireRawKinds(fields, map[string]rawKind{"file": rawString, "size": rawUnsigned, "sha256": rawString}); err != nil {
			return err
		}
	}
	if hasROM {
		fields, err := exactRawObject(rom, []string{"id", "role", "source_size", "file", "size", "sha256"}, nil)
		if err != nil {
			return err
		}
		if err := requireRawKinds(fields, map[string]rawKind{"id": rawString, "role": rawString, "source_size": rawUnsigned, "file": rawString, "size": rawUnsigned, "sha256": rawString}); err != nil {
			return err
		}
	}
	core, err := exactRawObject(descriptor["core"], []string{"id", "name", "description", "version"}, []string{"system"})
	if err != nil {
		return err
	}
	if err := requireRawKinds(core, map[string]rawKind{
		"id": rawString, "name": rawString, "description": rawString,
		"version": rawString,
	}); err != nil {
		return err
	}
	if system, ok := core["system"]; ok {
		if err := requireRawKind(system, rawString); err != nil {
			return err
		}
		var value string
		if json.Unmarshal(system, &value) != nil || value == "" {
			return errInvalidRuntimeResponse
		}
	}
	target, err := exactRawObject(descriptor["target"], []string{"platform", "device", "programming_profile"}, nil)
	if err != nil {
		return err
	}
	if err := requireRawKinds(target, map[string]rawKind{
		"platform": rawString, "device": rawString,
		"programming_profile": rawString,
	}); err != nil {
		return err
	}
	payload, err := exactRawObject(descriptor["payload"], []string{"file", "size", "sha256"}, nil)
	if err != nil {
		return err
	}
	if err := requireRawKinds(payload, map[string]rawKind{
		"file": rawString, "size": rawUnsigned, "sha256": rawString,
	}); err != nil {
		return err
	}
	abi, err := exactRawObject(descriptor["abi"], []string{"id", "major", "minor"}, nil)
	if err != nil {
		return err
	}
	if err := requireRawKinds(abi, map[string]rawKind{
		"id": rawString, "major": rawUnsigned, "minor": rawUnsigned,
	}); err != nil {
		return err
	}
	if err = eachRaw(descriptor["interfaces"], func(item json.RawMessage) error {
		fields, itemErr := exactRawObject(item, []string{"id", "major", "minor", "required"}, nil)
		if itemErr != nil {
			return itemErr
		}
		return requireRawKinds(fields, map[string]rawKind{
			"id": rawString, "major": rawUnsigned, "minor": rawUnsigned,
			"required": rawBoolean,
		})
	}); err != nil {
		return err
	}
	build, err := exactRawObject(descriptor["build"], []string{"id", "repository", "revision", "recipe_sha256", "toolchain"}, nil)
	if err != nil {
		return err
	}
	return requireRawKinds(build, map[string]rawKind{
		"id": rawString, "repository": rawString, "revision": rawString,
		"recipe_sha256": rawString, "toolchain": rawString,
	})
}

func validateErrorShape(raw json.RawMessage) error {
	object, err := exactRawObject(raw, []string{"code", "message", "phase"}, []string{"expected", "observed"})
	if err != nil {
		return err
	}
	if err := requireRawKinds(object, map[string]rawKind{
		"code": rawString, "message": rawString, "phase": rawString,
	}); err != nil {
		return err
	}
	for _, optional := range []string{"expected", "observed"} {
		if value, ok := object[optional]; ok {
			if err := requireRawKind(value, rawString); err != nil {
				return err
			}
			var text string
			if json.Unmarshal(value, &text) != nil || text == "" {
				return errInvalidRuntimeResponse
			}
		}
	}
	return nil
}

func validateSupportedInterfaceShape(raw json.RawMessage) error {
	object, err := exactRawObject(raw, []string{"id", "major", "minor"}, nil)
	if err != nil {
		return err
	}
	return requireRawKinds(object, map[string]rawKind{
		"id": rawString, "major": rawUnsigned, "minor": rawUnsigned,
	})
}

type rawKind uint8

const (
	rawObject rawKind = iota
	rawArray
	rawString
	rawBoolean
	rawUnsigned
	rawNullableObject
	rawNullableString
	rawNullableUnsigned
)

func requireRawKinds(object map[string]json.RawMessage, fields map[string]rawKind) error {
	for name, kind := range fields {
		value, ok := object[name]
		if !ok || requireRawKind(value, kind) != nil {
			return errInvalidRuntimeResponse
		}
	}
	return nil
}

func requireRawKind(raw json.RawMessage, kind rawKind) error {
	value := bytes.TrimSpace(raw)
	if len(value) == 0 {
		return errInvalidRuntimeResponse
	}
	if isNull(value) {
		switch kind {
		case rawNullableObject, rawNullableString, rawNullableUnsigned:
			return nil
		default:
			return errInvalidRuntimeResponse
		}
	}
	switch kind {
	case rawObject, rawNullableObject:
		if value[0] != '{' {
			return errInvalidRuntimeResponse
		}
	case rawArray:
		if value[0] != '[' {
			return errInvalidRuntimeResponse
		}
	case rawString, rawNullableString:
		if value[0] != '"' {
			return errInvalidRuntimeResponse
		}
	case rawBoolean:
		if !bytes.Equal(value, []byte("true")) && !bytes.Equal(value, []byte("false")) {
			return errInvalidRuntimeResponse
		}
	case rawUnsigned, rawNullableUnsigned:
		if value[0] < '0' || value[0] > '9' {
			return errInvalidRuntimeResponse
		}
	}
	return nil
}

func exactRawObject(raw json.RawMessage, required, optional []string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, name := range required {
		allowed[name] = true
		if _, ok := object[name]; !ok {
			return nil, errInvalidRuntimeResponse
		}
	}
	for _, name := range optional {
		allowed[name] = true
	}
	for name := range object {
		if !allowed[name] {
			return nil, errInvalidRuntimeResponse
		}
	}
	return object, nil
}

func eachRaw(raw json.RawMessage, visit func(json.RawMessage) error) error {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return err
	}
	for _, item := range items {
		if err := visit(item); err != nil {
			return err
		}
	}
	return nil
}

func isNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

func rejectDuplicateJSONNames(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errInvalidRuntimeResponse
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, structured := token.(json.Delim)
	if !structured {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			nameToken, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := nameToken.(string)
			if !ok || seen[name] {
				return errInvalidRuntimeResponse
			}
			seen[name] = true
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errInvalidRuntimeResponse
		}
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errInvalidRuntimeResponse
		}
	default:
		return errInvalidRuntimeResponse
	}
	return nil
}
