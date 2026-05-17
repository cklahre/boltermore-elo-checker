package bcp

import "testing"

func TestDetachmentFromArmyListJSON_topLevel(t *testing.T) {
	const j = `{"id":"x","detachment":"Gladius Task Force","factionId":"a"}`
	if got := DetachmentFromArmyListJSON([]byte(j)); got != "Gladius Task Force" {
		t.Fatalf("got %q", got)
	}
}

func TestDetachmentFromArmyListJSON_nestedArmy(t *testing.T) {
	const j = `{"id":"x","army":{"detachment":"Vanguard Onslaught"}}`
	if got := DetachmentFromArmyListJSON([]byte(j)); got != "Vanguard Onslaught" {
		t.Fatalf("got %q", got)
	}
}

func TestDetachmentFromArmyListJSON_subFactionObject(t *testing.T) {
	const j = `{"id":"x","subFaction":{"id":"abc","name":"Vanguard Onslaught"}}`
	if got := DetachmentFromArmyListJSON([]byte(j)); got != "Vanguard Onslaught" {
		t.Fatalf("got %q", got)
	}
}

func TestDetachmentFromArmyListJSON_subFactionNameField(t *testing.T) {
	const j = `{"subFactionName":"Gladius Task Force"}`
	if got := DetachmentFromArmyListJSON([]byte(j)); got != "Gladius Task Force" {
		t.Fatalf("got %q", got)
	}
}

func TestDetachmentFromArmyListJSON_warhammer10e(t *testing.T) {
	const j = `{"warhammer":{"detachment":"Coterie of the Conceited"}}`
	if got := DetachmentFromArmyListJSON([]byte(j)); got != "Coterie of the Conceited" {
		t.Fatalf("got %q", got)
	}
}

func TestDetachmentFromArmyListJSON_masterWarhammer(t *testing.T) {
	const j = `{"master":{"warhammer":{"detachment":"Gladius Task Force"}}}`
	if got := DetachmentFromArmyListJSON([]byte(j)); got != "Gladius Task Force" {
		t.Fatalf("got %q", got)
	}
}

func TestDetachmentFromArmyListText_fallback(t *testing.T) {
	const j = `{"armyListText":"Roster\n\nDetachment: Spearpoint Task Force\n\nUnits"}`
	if got := DetachmentFromArmyListJSON([]byte(j)); got != "Spearpoint Task Force" {
		t.Fatalf("got %q", got)
	}
}

func TestDetachmentFromArmyListJSON_armyListText_strikeForceBlock(t *testing.T) {
	const j = `{"armyListText":"Just The Tip (2000 points)\n\nSpace Marines\nSpace Wolves\nStrike Force (2000 points)\nArmoured Speartip\n\nCHARACTERS"}`
	if got := DetachmentFromArmyListJSON([]byte(j)); got != "Armoured Speartip" {
		t.Fatalf("got %q", got)
	}
}

func TestDetachmentFromArmyListText_berzerkerBeforeStrike(t *testing.T) {
	const j = `{"armyListText":"Waaagh\nWorld Eaters\nBerzerker Warband\nStrike Force (2000 points)\nCHARACTERS\n"}`
	if got := DetachmentFromArmyListJSON([]byte(j)); got != "Berzerker Warband" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeStoredDetachment_rejectsWarlordJunk(t *testing.T) {
	if g := normalizeStoredDetachment("• Warlord"); g != "" {
		t.Fatalf("got %q", g)
	}
}

func TestNormalizeDetachmentDisplay_stripsParenthetical(t *testing.T) {
	if g := NormalizeDetachmentDisplay("Starshatter Arsenal (Relentless Onslaught)"); g != "Starshatter Arsenal" {
		t.Fatalf("got %q", g)
	}
	if g := NormalizeDetachmentDisplay("Starshatter Arsenal (Relentless Onslaught)"); g != "Starshatter Arsenal" {
		t.Fatalf("got %q", g)
	}
}

func TestDetachmentFromArmyListText_skipsCharactersHeading(t *testing.T) {
	const onlyChar = `{"armyListText":"Strike Force (2000 points)\nCHARACTERS\n"}`
	if got := DetachmentFromArmyListJSON([]byte(onlyChar)); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	const withReal = `{"armyListText":"Strike Force (2000 points)\nCHARACTERS\nArmoured Speartip\n"}`
	if got := DetachmentFromArmyListJSON([]byte(withReal)); got != "Armoured Speartip" {
		t.Fatalf("got %q", got)
	}
}
