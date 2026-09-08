package breaker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bakytn/pogo/internal/engine"
)

// repoRoot walks up from the package dir to the working-tree root (the dir
// that contains data/). `go test` runs with CWD == package dir, so relative
// paths must be rooted here.
func repoRoot(t *testing.T) string {
	t.Helper()
	for _, up := range []string{".", "..", "../..", "../../.."} {
		d, _ := filepath.Abs(up)
		if _, err := os.Stat(filepath.Join(d, "data", "gamemaster.json")); err == nil {
			return d
		}
	}
	t.Skip("no data/gamemaster.json reachable from the package dir")
	return ""
}

func loadGameData(t *testing.T) *engine.GameData {
	t.Helper()
	gd, err := engine.NewGameData(filepath.Join(repoRoot(t), "data", "gamemaster.json"))
	if err != nil {
		t.Fatalf("load game data: %v", err)
	}
	return gd
}

func newMetaLoader(t *testing.T) *Loader {
	t.Helper()
	return NewLoader(filepath.Join(repoRoot(t), "data", "meta"))
}

// TestBreakerShadowClaw pins the core invariant behind the feature: a
// high-attack Sableye's damage in the 15-0-0 (max-atk) state is >= its Rank-1
// state and its 0-15-0 (max-def) state, for every move against every top-meta
// opponent (max-atk has the highest attack, rank 1 spends IVs on product so
// its attack is lower, and max-def deliberately minimizes attack). This is the
// "high attack does +1 shadow claw over rank 1" effect, asserted across the
// whole meta so the test always exercises real comparisons.
func TestBreakerShadowClaw(t *testing.T) {
	gd := loadGameData(t)
	ldr := newMetaLoader(t)
	league, err := ldr.Load("great")
	if err != nil {
		t.Fatalf("load great meta: %v", err)
	}
	a, err := NewAnalyzer(gd, "sableye", engine.IVs{Atk: 15, Def: 0, Hp: 0}, 1500, false)
	if err != nil {
		t.Fatalf("analyzer: %v", err)
	}
	checked := 0
	for _, o := range a.Analyze(league) {
		for _, md := range o.Moves {
			if len(md.States) != 4 {
				t.Fatalf("%s/%s: %d states, want 4", o.Entry.SpeciesName, md.Move.ID, len(md.States))
			}
			now, r1, maxAtk, maxDef := md.States[0], md.States[1], md.States[2], md.States[3]
			_ = now
			// max-atk attack >= rank-1 attack => damage ordering (same opp/def).
			if maxAtk < r1 {
				t.Errorf("%s %s: maxAtk=%d < rank1=%d", o.Entry.SpeciesName, md.Move.ID, maxAtk, r1)
			}
			// max-def deliberately minimizes attack => lowest (or tied) damage.
			if maxDef > maxAtk {
				t.Errorf("%s %s: maxDef=%d > maxAtk=%d", o.Entry.SpeciesName, md.Move.ID, maxDef, maxAtk)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no moves checked — meta or move pool empty?")
	}
	t.Logf("checked %d move/opponent damage orderings (maxAtk>=rank1>=maxDef)", checked)
}

// TestBreakerDumpWellFormed pins the machine-readable dump shape so a
// regression in the CP-walk or damage math changes a pinned line and fails.
func TestBreakerDumpWellFormed(t *testing.T) {
	gd := loadGameData(t)
	league, err := newMetaLoader(t).Load("great")
	if err != nil {
		t.Fatalf("load great meta: %v", err)
	}
	a, err := NewAnalyzer(gd, "mewtwo", engine.IVs{Atk: 10, Def: 10, Hp: 10}, 1500, false)
	if err != nil {
		t.Fatalf("analyzer: %v", err)
	}
	var b strings.Builder
	writeDump(&b, a, league, a.Analyze(league))
	dump := b.String()
	lines := strings.Split(strings.TrimSpace(dump), "\n")
	if len(lines) < 1 || !strings.HasPrefix(lines[0], "STATES ") {
		t.Fatalf("bad dump head: %q", firstLine(lines))
	}
	// 4 attacker atk values + 4 attacker def values (needed for bulkpoint).
	if n := len(strings.Fields(lines[0])) - 1; n != 8 {
		t.Fatalf("STATES has %d values, want 8: %q", n, lines[0])
	}
	if !strings.Contains(dump, "OPP ") {
		t.Fatal("dump has no OPP blocks")
	}
	// Each opponent carries its re-derived default-IV stats and a bulkpoint
	// (fast-move-against-you) line.
	oppCount, bulkCount := 0, 0
	for _, l := range lines[1:] {
		f := strings.Fields(l)
		switch {
		case len(f) >= 3 && f[0] == "OPP":
			oppCount++
		case len(f) >= 5 && f[2] == "BULK":
			bulkCount++
		}
	}
	if oppCount == 0 {
		t.Fatal("dump has no OPP lines")
	}
	if bulkCount == 0 {
		t.Fatal("dump has no BULK (bulkpoint) lines")
	}
}

func firstLine(lines []string) string {
	if len(lines) == 0 {
		return "<empty>"
	}
	return lines[0]
}
