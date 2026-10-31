package hostclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/DeanoC/FogCast/protocol"
)

// CoreExpansion is an imported archive's exact shell and physical socket.
// Selection still asks the host to admit the immutable archive.
type CoreExpansion struct {
	ExpansionID string `json:"expansion_id"`
	PackageID   string `json:"package_id"`
	Slot        int    `json:"slot,omitempty"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
}

type CoreEntrySlotExpansions struct {
	GameID     string          `json:"game_id"`
	PackageID  string          `json:"package_id"`
	Sockets    []int           `json:"sockets"`
	Expansions []SlotExpansion `json:"expansions"`
	Ready      bool            `json:"ready"`
}

func (c *Client) CoreExpansions(ctx context.Context) ([]CoreExpansion, error) {
	var result []CoreExpansion
	if err := c.getJSON(ctx, "/api/v1/core-expansions", &result); err != nil {
		return nil, err
	}
	for _, card := range result {
		if protocol.ValidateDigest(card.ExpansionID) != nil || protocol.ValidateDigest(card.PackageID) != nil || card.Slot < 0 || card.Slot > 7 {
			return nil, fmt.Errorf("host returned an invalid expansion identity")
		}
	}
	return result, nil
}

func (c *Client) CoreEntryExpansion(ctx context.Context, game string) (CoreEntryExpansion, error) {
	if protocol.ValidateGameID(game) != nil {
		return CoreEntryExpansion{}, fmt.Errorf("invalid game identity")
	}
	var result CoreEntryExpansion
	if err := c.getJSON(ctx, "/api/v1/library/core-entries/"+url.PathEscape(game)+"/expansion", &result); err != nil {
		return result, err
	}
	if result.GameID != game || result.ExpansionID != "" && protocol.ValidateDigest(result.ExpansionID) != nil {
		return result, fmt.Errorf("host returned a different expansion selection")
	}
	return result, nil
}

func (c *Client) CoreEntrySlotExpansions(ctx context.Context, game string) (CoreEntrySlotExpansions, error) {
	if protocol.ValidateGameID(game) != nil {
		return CoreEntrySlotExpansions{}, fmt.Errorf("invalid game identity")
	}
	var result CoreEntrySlotExpansions
	if err := c.getJSON(ctx, "/api/v1/library/core-entries/"+url.PathEscape(game)+"/expansions", &result); err != nil {
		return result, err
	}
	return result, validateCoreEntrySlots(result, game, "")
}

func validateCoreEntrySlots(value CoreEntrySlotExpansions, game, pkg string) error {
	if value.GameID != game || protocol.ValidateDigest(value.PackageID) != nil || pkg != "" && value.PackageID != pkg {
		return fmt.Errorf("host returned a different slot selection")
	}
	sockets := map[int]bool{}
	last := 0
	for _, slot := range value.Sockets {
		if slot <= last || slot > 7 {
			return fmt.Errorf("host returned an invalid physical socket")
		}
		sockets[slot], last = true, slot
	}
	seen := map[int]bool{}
	for _, card := range value.Expansions {
		if !sockets[card.Slot] || seen[card.Slot] || protocol.ValidateDigest(card.ExpansionID) != nil {
			return fmt.Errorf("host returned an invalid slot selection")
		}
		seen[card.Slot] = true
	}
	return nil
}

func (c *Client) SelectCoreEntrySlotExpansion(ctx context.Context, game, pkg string, slot int, expected, id string) (CoreEntrySlotExpansions, error) {
	if protocol.ValidateGameID(game) != nil || protocol.ValidateDigest(pkg) != nil || slot < 1 || slot > 7 ||
		expected != "" && protocol.ValidateDigest(expected) != nil || id != "" && protocol.ValidateDigest(id) != nil {
		return CoreEntrySlotExpansions{}, fmt.Errorf("invalid slot selection identity")
	}
	var result CoreEntrySlotExpansions
	payload := map[string]string{"package_id": pkg, "expected_expansion_id": expected, "expansion_id": id}
	path := "/api/v1/library/core-entries/" + url.PathEscape(game) + "/expansions/" + strconv.Itoa(slot)
	if err := c.doJSON(ctx, http.MethodPut, path, payload, &result); err != nil {
		return result, err
	}
	if err := validateCoreEntrySlots(result, game, pkg); err != nil {
		return result, err
	}
	selected := ""
	validSlot := false
	for _, socket := range result.Sockets {
		validSlot = validSlot || socket == slot
	}
	for _, card := range result.Expansions {
		if card.Slot == slot {
			selected = card.ExpansionID
		}
	}
	if !validSlot || selected != id {
		return result, fmt.Errorf("host returned a different slot selection")
	}
	return result, nil
}
