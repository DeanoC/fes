package misterruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"regexp"
	"sort"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

var errProtocol2Unsupported = errors.New("runtime protocol 2 is unsupported")

type protocol2MutationError struct {
	error
	attempted bool
}

func (err protocol2MutationError) Unwrap() error { return err.error }

var (
	protocol2Identifier = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,95}$`)
	protocol2Hex64      = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Protocol2Error struct {
	Code     string  `json:"code"`
	Message  string  `json:"message"`
	Phase    string  `json:"phase"`
	Expected *string `json:"expected,omitempty"`
	Observed *string `json:"observed,omitempty"`
}

type Protocol2Response struct {
	CoreData         *protocol.CoreData      `json:"core_data,omitempty"`
	Protocol         int                     `json:"protocol"`
	OK               bool                    `json:"ok"`
	State            string                  `json:"state"`
	Execution        string                  `json:"execution"`
	System           *string                 `json:"system"`
	Core             *string                 `json:"core"`
	Error            *Protocol2Error         `json:"error"`
	Version          string                  `json:"version"`
	Capabilities     Protocol2Capabilities   `json:"capabilities"`
	ActivePackage    *Protocol2ActivePackage `json:"active_package"`
	Generation       *uint64                 `json:"generation"`
	InspectedPackage *Protocol2Inspection    `json:"inspected_package"`
}

func (client *Client) SetKeyboard(ctx context.Context, matrix uint64) (Protocol2Response, error) {
	if matrix > 0xffffffffff {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, err := client.callRaw(ctx, struct {
		Protocol  int    `json:"protocol"`
		Operation string `json:"operation"`
		Matrix    uint64 `json:"matrix"`
	}{Protocol: 2, Operation: "set_keyboard", Matrix: matrix})
	if err != nil {
		return Protocol2Response{}, err
	}
	return decodeProtocol2Response(line)
}

func (client *Client) Protocol2Status(ctx context.Context) (Protocol2Response, error) {
	line, err := client.callRaw(ctx, struct {
		Protocol  int    `json:"protocol"`
		Operation string `json:"operation"`
	}{Protocol: 2, Operation: "status"})
	if err != nil {
		return Protocol2Response{}, err
	}
	response, err := decodeNegotiationResponse(line)
	if err != nil {
		return Protocol2Response{}, err
	}
	if response.InspectedPackage != nil || (!response.OK && response.State != "reboot_required" && !protocol2ResumedSaveFailure(response)) {
		return Protocol2Response{}, errInvalidRuntimeResponse
	}
	return response, nil
}

func protocol2ResumedSaveFailure(response Protocol2Response) bool {
	return response.Error != nil && response.Error.Code == "save_failed" && response.Error.Phase == "save" &&
		(response.State == "running_development")
}

func describedPackageRuntimeState(response Protocol2Response) bool {
	return response.State == "running_development" || (response.State == "reboot_required" && response.ActivePackage != nil)
}

func (client *Client) LoadCore(ctx context.Context, path, packageID string) (Protocol2Response, error) {
	response, _, err := client.loadCoreWithStatus(ctx, path, packageID)
	return response, err
}

func (client *Client) loadCoreWithStatus(ctx context.Context, path, packageID string) (Protocol2Response, Protocol2Response, error) {
	return client.loadCoreWithRoot(ctx, path, packageID, "")
}
func (client *Client) loadCoreOperation(ctx context.Context, path, id, root string) (Protocol2Response, error) {
	if !validRuntimePath(root) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	response, _, err := client.loadCoreWithRoot(ctx, path, id, root)
	return response, err
}
func (client *Client) loadCoreWithRoot(ctx context.Context, path, packageID, root string) (Protocol2Response, Protocol2Response, error) {
	return client.loadCoreWithComposition(ctx, path, packageID, root, "", "", nil)
}
func (client *Client) LoadComposedCore(ctx context.Context, path, packageID, expansionPath, payloadPath string, composition expansion.Composition) (Protocol2Response, error) {
	if !validRuntimePath(expansionPath) || !validRuntimePath(payloadPath) || !validComposition(composition, packageID) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	response, _, err := client.loadCoreWithComposition(ctx, path, packageID, "", expansionPath, payloadPath, &composition)
	return response, err
}
func (client *Client) loadCoreWithComposition(ctx context.Context, path, packageID, root, expansionPath, payloadPath string, composition *expansion.Composition) (Protocol2Response, Protocol2Response, error) {
	if !validRuntimePath(path) || !protocol2Hex64.MatchString(packageID) {
		return Protocol2Response{}, Protocol2Response{}, errInvalidRuntimeRequest
	}
	before, err := client.Protocol2Status(ctx)
	if err != nil {
		return Protocol2Response{}, Protocol2Response{}, protocol2MutationError{error: err, attempted: false}
	}
	operation := "load_core"
	if composition != nil {
		operation = "load_composed_core"
	}
	if root != "" {
		operation = "load_library_core"
	}
	line, attempted, err := client.callRawTracked(ctx, struct {
		Protocol      int                    `json:"protocol"`
		Operation     string                 `json:"operation"`
		PackagePath   string                 `json:"package_path"`
		PackageID     string                 `json:"package_id"`
		DataRoot      string                 `json:"data_root,omitempty"`
		ExpansionPath string                 `json:"expansion_path,omitempty"`
		PayloadPath   string                 `json:"payload_path,omitempty"`
		Composition   *expansion.Composition `json:"composition,omitempty"`
	}{Protocol: 2, Operation: operation, PackagePath: path, PackageID: packageID, DataRoot: root, ExpansionPath: expansionPath, PayloadPath: payloadPath, Composition: composition})
	if err != nil {
		return Protocol2Response{}, before, protocol2MutationError{error: err, attempted: attempted}
	}
	response, err := decodeProtocol2Response(line)
	if err != nil || response.InspectedPackage != nil {
		if err != nil {
			return Protocol2Response{}, before, protocol2MutationError{error: err, attempted: true}
		}
		return Protocol2Response{}, before, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: true}
	}
	if response.OK && (response.State != "running_development" ||
		response.Execution != "development" || response.ActivePackage == nil ||
		response.ActivePackage.PackageID != packageID || !reflect.DeepEqual(response.ActivePackage.Composition, composition)) {
		return Protocol2Response{}, before, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: true}
	}
	return response, before, nil
}

func (client *Client) InspectCore(ctx context.Context, path, packageID string) (Protocol2Response, error) {
	if !validRuntimePath(path) || !protocol2Hex64.MatchString(packageID) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, err := client.callRaw(ctx, struct {
		Protocol    int    `json:"protocol"`
		Operation   string `json:"operation"`
		PackagePath string `json:"package_path"`
		PackageID   string `json:"package_id"`
	}{Protocol: 2, Operation: "inspect_core", PackagePath: path, PackageID: packageID})
	if err != nil {
		return Protocol2Response{}, err
	}
	response, err := decodeProtocol2Response(line)
	if err != nil {
		return Protocol2Response{}, err
	}
	if response.OK != (response.InspectedPackage != nil) ||
		(response.OK && response.InspectedPackage.PackageID != packageID) {
		return Protocol2Response{}, errInvalidRuntimeResponse
	}
	return response, nil
}

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
	if !validProtocol2Response(response) {
		return Protocol2Response{}, errInvalidRuntimeResponse
	}
	return response, nil
}

func validateProtocol2Shape(line []byte) error {
	var root json.RawMessage = line
	object, err := exactRawObject(root, []string{"protocol", "ok", "state", "execution", "system", "core", "error", "version", "capabilities", "active_package", "generation", "inspected_package"}, []string{"core_data"})
	if err != nil {
		return err
	}
	if err := requireRawKinds(object, map[string]rawKind{
		"protocol": rawUnsigned, "ok": rawBoolean, "state": rawString,
		"execution": rawString, "system": rawNullableString,
		"core": rawNullableString, "error": rawNullableObject,
		"version": rawString, "capabilities": rawObject,
		"active_package": rawNullableObject, "generation": rawNullableUnsigned,
		"inspected_package": rawNullableObject,
	}); err != nil {
		return err
	}
	if data, ok := object["core_data"]; ok && !isNull(data) {
		if err := validateCoreDataShape(data); err != nil {
			return err
		}
	}
	if !isNull(object["error"]) {
		if err := validateErrorShape(object["error"]); err != nil {
			return err
		}
	}
	capabilities, err := exactRawObject(object["capabilities"], []string{"programming_profiles", "abis", "active_interfaces"}, []string{"media_stream"})
	if err != nil {
		return err
	}
	if err := requireRawKinds(capabilities, map[string]rawKind{
		"programming_profiles": rawArray, "abis": rawArray,
		"active_interfaces": rawArray,
	}); err != nil {
		return err
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
		active, err := exactRawObject(object["active_package"], []string{"package_id", "descriptor", "observed"}, []string{"persistence_mode", "composition"})
		if err != nil {
			return err
		}
		if err := requireRawKinds(active, map[string]rawKind{
			"package_id": rawString, "descriptor": rawObject,
			"observed": rawObject,
		}); err != nil {
			return err
		}
		if value, ok := active["composition"]; ok && !isNull(value) {
			tuple, err := exactRawObject(value, []string{"composition_id", "package_id", "expansion_id", "shell_sha256", "payload_sha256", "payload_size"}, nil)
			if err != nil {
				return err
			}
			if err := requireRawKinds(tuple, map[string]rawKind{"composition_id": rawString, "package_id": rawString, "expansion_id": rawString, "shell_sha256": rawString, "payload_sha256": rawString, "payload_size": rawUnsigned}); err != nil {
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

func validateDescriptorShape(raw json.RawMessage) error {
	descriptor, err := exactRawObject(raw, []string{"format", "core", "target", "payload", "abi", "interfaces", "build"}, nil)
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

func validProtocol2Response(response Protocol2Response) bool {
	if response.Capabilities.MediaStream != nil {
		if response.ActivePackage == nil || !positive(response.Generation) {
			return false
		}
		activation := activationFromProtocol2(response.ActivePackage.PackageID, response.ActivePackage.Descriptor, response)
		if !protocol.MediaStreamCapable(corePackageStatus(activation)) {
			return false
		}
		declared := protocol.DeclaredCoreMediaCapabilities(response.ActivePackage.Descriptor)
		if len(declared) != 1 || declared[0].Interface != protocol.MediaStreamInterface() {
			return false
		}
	}
	if response.CoreData != nil && !response.CoreData.Valid() {
		return false
	}
	if response.Protocol != 2 || response.Version == "" ||
		(response.Error == nil && !response.OK) ||
		(response.Error != nil && !validProtocol2Error(*response.Error)) ||
		!validCapabilities(response.Capabilities) ||
		(response.InspectedPackage != nil && !response.OK) {
		return false
	}
	identity := protocol2Identity(response.System, response.Core)
	noActive := response.ActivePackage == nil && len(response.Capabilities.ActiveInterfaces) == 0
	switch response.State {
	case "idle":
		if response.Execution != "none" || identity != identityNone || response.Generation != nil || !noActive {
			return false
		}
	case "starting":
		if response.Generation != nil || !noActive {
			return false
		}
		switch response.Execution {
		case "none":
			if identity != identityNone {
				return false
			}
		case "development":
			if identity != identityNone {
				return false
			}
		default:
			return false
		}
	case "running_development":
		if response.Execution != "development" || (identity != identityNone && identity != identityCore) || !positive(response.Generation) {
			return false
		}
		if response.ActivePackage == nil {
			if identity != identityNone || len(response.Capabilities.ActiveInterfaces) != 0 {
				return false
			}
		} else if !validActivePackage(*response.ActivePackage, response.Capabilities) {
			return false
		}
	case "reboot_required":
		if response.Error == nil || response.Error.Code != "idle_failed" {
			return false
		}
		if response.ActivePackage != nil {
			if response.Execution != "development" || identity != identityCore || !positive(response.Generation) || response.ActivePackage.PersistenceMode != "persistent" || response.Error.Phase != "recovery" || !validActivePackage(*response.ActivePackage, response.Capabilities) {
				return false
			}
		} else if response.Execution != "none" || identity != identityNone || response.Generation != nil || !noActive {
			return false
		}
	default:
		return false
	}
	if response.InspectedPackage != nil && !validInspection(*response.InspectedPackage, response.Capabilities) {
		return false
	}
	return true
}

func validProtocol2Error(remote Protocol2Error) bool {
	switch remote.Code {
	case "invalid_request", "unsupported_protocol", "unknown_system", "missing_media", "busy", "program_failed", "core_mismatch", "io_failed", "idle_failed", "save_failed", "invalid_package", "unsupported_target", "unsupported_programming_profile", "unsupported_abi", "unsupported_interface", "corrupt_data", "incompatible_data", "stale_revision":
	default:
		return false
	}
	switch remote.Phase {
	case "core_data", "request", "admission", "compatibility", "save", "quiesce", "programming", "transport", "identity", "video", "input", "recovery", "lifecycle":
		return true
	default:
		return false
	}
}

func validCapabilities(capabilities Protocol2Capabilities) bool {
	if !sortedUniqueStrings(capabilities.ProgrammingProfiles) ||
		!sort.SliceIsSorted(capabilities.ABIs, func(i, j int) bool { return capabilities.ABIs[i].ID < capabilities.ABIs[j].ID }) {
		return false
	}
	seen := map[string]bool{}
	for _, abi := range capabilities.ABIs {
		if seen[abi.ID] || !validContract(abi.ID, abi.Major, abi.Minor) || !validSupportedInterfaces(abi.Interfaces) {
			return false
		}
		seen[abi.ID] = true
	}
	return validSupportedInterfaces(capabilities.ActiveInterfaces)
}

func validSupportedInterfaces(interfaces []Protocol2Interface) bool {
	if !sort.SliceIsSorted(interfaces, func(i, j int) bool { return interfaces[i].ID < interfaces[j].ID }) {
		return false
	}
	seen := map[string]bool{}
	for _, value := range interfaces {
		if seen[value.ID] || !validContract(value.ID, value.Major, value.Minor) {
			return false
		}
		seen[value.ID] = true
	}
	return true
}

func validActivePackage(active Protocol2ActivePackage, capabilities Protocol2Capabilities) bool {
	if c := active.Composition; c != nil {
		if !validComposition(*c, active.PackageID) || c.ShellSHA256 != active.Descriptor.Payload.SHA256 || active.PersistenceMode == "persistent" || active.Descriptor.ABI.ID != "fes.simple-computer" || active.Descriptor.ABI.Major != 1 || active.Descriptor.ABI.Minor != 0 {
			return false
		}
		socket := false
		for _, i := range active.Descriptor.Interfaces {
			if i.ID == "fes.expansion.zx81-bus" && i.Major == 1 && i.Minor == 0 && !i.Required {
				socket = true
			}
		}
		if !socket {
			return false
		}
	}
	if !protocol2Hex64.MatchString(active.PackageID) || !validDescriptor(active.Descriptor) {
		return false
	}
	abi := matchingABI(capabilities, active.Descriptor)
	if abi == nil || !containsString(capabilities.ProgrammingProfiles,
		active.Descriptor.Target.ProgrammingProfile) ||
		!requiredInterfacesSupported(active.Descriptor, *abi) ||
		!equalInterfaces(capabilities.ActiveInterfaces,
			interfaceIntersection(active.Descriptor, *abi)) {
		return false
	}
	if (active.Observed.ABI == nil) != (active.Observed.BuildID == nil) {
		return false
	}
	switch active.Descriptor.Target.ProgrammingProfile {
	case "fes-gp-v1":
		if active.Observed.ABI == nil {
			return false
		}
	default:
		return false
	}
	if active.Observed.ABI == nil {
		return true
	}
	return active.Observed.ABI.ID == active.Descriptor.ABI.ID &&
		int64(active.Observed.ABI.Major) == active.Descriptor.ABI.Major &&
		int64(active.Observed.ABI.Minor) == active.Descriptor.ABI.Minor &&
		*active.Observed.BuildID == active.Descriptor.Build.ID
}

func validInspection(inspection Protocol2Inspection, capabilities Protocol2Capabilities) bool {
	if layout := inspection.PersistenceLayout; layout != nil {
		found := false
		for _, i := range inspection.Descriptor.Interfaces {
			if i.Required && i.ID == layout.ID && i.Major == int64(layout.Major) && i.Minor == int64(layout.Minor) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	if !protocol2Hex64.MatchString(inspection.PackageID) || !validDescriptor(inspection.Descriptor) {
		return false
	}
	if inspection.Compatible {
		abi := matchingABI(capabilities, inspection.Descriptor)
		return inspection.CompatibilityError == nil && abi != nil &&
			containsString(capabilities.ProgrammingProfiles,
				inspection.Descriptor.Target.ProgrammingProfile) &&
			requiredInterfacesSupported(inspection.Descriptor, *abi)
	}
	return inspection.CompatibilityError != nil && validProtocol2Error(*inspection.CompatibilityError)
}

func matchingABI(capabilities Protocol2Capabilities, descriptor corepackage.Descriptor) *Protocol2ABI {
	for index := range capabilities.ABIs {
		candidate := &capabilities.ABIs[index]
		if candidate.ID == descriptor.ABI.ID && int64(candidate.Major) == descriptor.ABI.Major &&
			int64(candidate.Minor) == descriptor.ABI.Minor {
			return candidate
		}
	}
	return nil
}

func requiredInterfacesSupported(descriptor corepackage.Descriptor, abi Protocol2ABI) bool {
	for _, declared := range descriptor.Interfaces {
		if !declared.Required {
			continue
		}
		found := false
		for _, supported := range abi.Interfaces {
			if supported.ID == declared.ID && int64(supported.Major) == declared.Major &&
				int64(supported.Minor) == declared.Minor {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func interfaceIntersection(descriptor corepackage.Descriptor, abi Protocol2ABI) []Protocol2Interface {
	var result []Protocol2Interface
	for _, declared := range descriptor.Interfaces {
		for _, supported := range abi.Interfaces {
			if supported.ID == declared.ID && int64(supported.Major) == declared.Major &&
				int64(supported.Minor) == declared.Minor {
				result = append(result, supported)
				break
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func equalInterfaces(left, right []Protocol2Interface) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func containsString(values []string, wanted string) bool {
	index := sort.SearchStrings(values, wanted)
	return index < len(values) && values[index] == wanted
}

func validDescriptor(descriptor corepackage.Descriptor) bool {
	return corepackage.ValidateDescriptor(descriptor) == nil
}

func validContract(id string, major, minor uint16) bool {
	return protocol2Identifier.MatchString(id) && major > 0
}

func sortedUniqueStrings(values []string) bool {
	if !sort.StringsAreSorted(values) {
		return false
	}
	for index, value := range values {
		if !protocol2Identifier.MatchString(value) || (index > 0 && values[index-1] == value) {
			return false
		}
	}
	return true
}

func positive(value *uint64) bool { return value != nil && *value != 0 }

func protocol2MutationAttempted(err error) bool {
	var attempted protocol2MutationError
	if errors.As(err, &attempted) {
		return attempted.attempted
	}
	// Alternate controls cannot prove that a transport error happened before
	// dispatch, so their ordinary errors remain sent-or-ambiguous.
	return true
}

func protocol2Identity(system, core *string) identityShape {
	if system == nil && core == nil {
		return identityNone
	}
	if system == nil && core != nil && *core != "" {
		return identityCore
	}
	if system == nil || core == nil || *system == "" || *core == "" {
		return identityInvalid
	}
	return identityPair
}

func validComposition(c expansion.Composition, packageID string) bool {
	id, err := expansion.CompositionID(c.PackageID, c.ExpansionID, c.PayloadSHA256)
	return err == nil && id == c.ID && c.PackageID == packageID && protocol2Hex64.MatchString(c.ShellSHA256) && c.PayloadSize >= 40408 && c.PayloadSize <= corepackage.MaxPayloadSize
}

// Protocol2LoadDevelopmentRBF admits only the contained diagnostic profile.
func (client *Client) Protocol2LoadDevelopmentRBF(ctx context.Context, rbf string) (Protocol2Response, error) {
	if !validRuntimePath(rbf) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, attempted, err := client.callRawTracked(ctx, struct {
		Protocol           int    `json:"protocol"`
		Operation          string `json:"operation"`
		RBF                string `json:"rbf"`
		ProgrammingProfile string `json:"programming_profile"`
	}{2, "load_development_rbf", rbf, "development-contained-v1"})
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: attempted}
	}
	response, err := decodeProtocol2Response(line)
	if err != nil || response.InspectedPackage != nil || response.CoreData != nil || (response.OK && !validDevelopmentRunning(response)) {
		return Protocol2Response{}, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: true}
	}
	return response, nil
}

type identityShape uint8

const (
	identityNone identityShape = iota
	identityPair
	identityCore
	identityInvalid
)
