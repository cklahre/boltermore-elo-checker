package bcp

import "testing"

func TestFactionCounts_armyAndUnknown(t *testing.T) {
	roster := []RosterPlayer{
		{Faction: &FactionRef{Name: "Orks"}},
		{Faction: &FactionRef{Name: "Orks"}},
		{Dropped: true, Faction: &FactionRef{Name: "Adeptus Custodes"}},
		{User: &bcpUser{FirstName: "X", LastName: "Y"}},
	}
	rows := FactionCounts(roster, func(p RosterPlayer) string { return p.ArmyFactionName() })
	if len(rows) < 3 {
		t.Fatalf("got %d rows, want at least 3", len(rows))
	}
	if rows[0].Label != "Orks" || rows[0].Count != 2 {
		t.Fatalf("first row: %+v", rows[0])
	}
	last := rows[len(rows)-1]
	if last.Label != "(no faction on roster)" || last.Count != 1 {
		t.Fatalf("last row: %+v", last)
	}
}

func TestRollupFactionName_parentPreferred(t *testing.T) {
	p := RosterPlayer{
		Faction: &FactionRef{
			Name:          "Grey Knights",
			ParentFaction: &FactionRef{Name: "Imperium"},
		},
	}
	if got := p.RollupFactionName(); got != "Imperium" {
		t.Fatalf("got %q", got)
	}
}

func TestArmyDetachmentTree_groupsAndSorts(t *testing.T) {
	det := map[string]string{"L1": "Gladius"}
	failed := map[string]struct{}{}
	roster := []RosterPlayer{
		{Faction: &FactionRef{Name: "Orks"}, ListID: "L1"},
		{Faction: &FactionRef{Name: "Orks"}, ListID: "L1"},
		{Faction: &FactionRef{Name: "Necrons"}, ListID: "L2"},
	}
	g := ArmyDetachmentTree(roster, det, failed)
	if len(g) != 2 {
		t.Fatalf("got %d groups", len(g))
	}
	if g[0].Army != "Necrons" || len(g[0].Lines) != 1 {
		t.Fatalf("first group: %+v", g[0])
	}
	// Orks: two Gladius
	var orks *ArmyDetachmentGroup
	for i := range g {
		if g[i].Army == "Orks" {
			orks = &g[i]
			break
		}
	}
	if orks == nil || len(orks.Lines) != 1 || orks.Lines[0].Label != "Gladius" || orks.Lines[0].Count != 2 {
		t.Fatalf("orks group: %+v", orks)
	}
}
