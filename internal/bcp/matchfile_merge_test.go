package bcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMatchesManifestPathList(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(dir, "a.json")
	if err := os.WriteFile(oldPath, []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}
	recentPath := filepath.Join(dir, "sub", "recent.json")
	if err := os.WriteFile(recentPath, []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}

	mf := filepath.Join(dir, "m.txt")
	manifestBody := `# comment
	a.json



sub/recent.json
`
	if err := os.WriteFile(mf, []byte(manifestBody), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := ParseMatchesManifestPathList(mf)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("len=%d paths=%v", len(paths), paths)
	}
	if paths[0] != oldPath || paths[1] != recentPath {
		t.Fatalf("got %v want %v then %v", paths, oldPath, recentPath)
	}
}

func TestMergeMatchFilesDedupe(t *testing.T) {
	dir := t.TempDir()
	for _, spec := range []struct {
		name string
		body string
	}{
		{"alpha.json", `[{"date":"2025-06-01T12:00:00Z","a":"A","b":"B","winner":"a","event_id":"e1","pairing_id":"same"},{"date":"2025-06-02T12:00:00Z","a":"C","b":"D","winner":"b","event_id":"e2","pairing_id":"x2"}]`},
		{"beta.json", `[{"date":"2025-06-01T15:00:00Z","a":"A","b":"B","winner":"b","event_id":"e1","pairing_id":"same"},{"date":"2025-06-03T12:00:00Z","a":"E","b":"F","winner":"a","event_id":"e3","pairing_id":"x3"}]`},
	} {
		p := filepath.Join(dir, spec.name)
		if err := os.WriteFile(p, []byte(spec.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	merged, err := MergeMatchFiles([]string{
		filepath.Join(dir, "alpha.json"),
		filepath.Join(dir, "beta.json"),
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 3 {
		t.Fatalf("dedupe merge: want 3 rows got %d", len(merged))
	}
}
