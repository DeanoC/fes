// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import "testing"

func TestPairedStation(t *testing.T) {
	const id = "4eb59443-8075-4dde-abcd-90abcdef1234"
	got, err := pairedStation(id)
	if err != nil || got != "8e:17:a1:8e:5c:93" {
		t.Fatal(got, err)
	}
	if other, err := pairedStation("4eb59443-8075-4dde-abcd-90abcdef1235"); err != nil || got == other {
		t.Fatal(other, err)
	}
	for _, invalid := range []string{"", "00000000-0000-0000-0000-000000000000", "private.bearer"} {
		if _, err := pairedStation(invalid); err == nil {
			t.Fatal("accepted invalid station identity")
		}
	}
}
