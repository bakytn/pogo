package engine

import (
	"math"
	"os"
	"testing"
)

// testGameData loads the real pvpoke gamemaster.json. It is byte-identical to
// the file served by pvpoke (kept in ./data so tests run offline).
func testGameData(t *testing.T) *GameData {
	t.Helper()
	p := FindDataFile()
	if p == "" {
		t.Skip("no gamemaster.json on disk (run `pogo -fetch rank ...` once)")
	}
	gd, err := NewGameData(p)
	if err != nil {
		t.Fatalf("load game data: %v", err)
	}
	return gd
}

// FindDataFile locates gamemaster.json relative to the working tree.
func FindDataFile() string {
	for _, p := range []string{
		"data/gamemaster.json",
		"../data/gamemaster.json",
		"../../data/gamemaster.json",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// lookup finds a species by exact name.
func lookup(gd *GameData, name string) *Pokemon {
	for i := range gd.Pokemon {
		if gd.Pokemon[i].SpeciesName == name {
			return &gd.Pokemon[i]
		}
	}
	return nil
}

func TestCpmTable(t *testing.T) {
	if len(cpm) != 109 {
		t.Fatalf("cpm table has %d entries, want 109", len(cpm))
	}
	// Level 1 (idx 0) and level 50 (idx 98 = (50-1)*2).
	if math.Abs(cpForLevel(1.0)-cpm[0]) > 1e-12 {
		t.Errorf("cpForLevel(1.0) = %v, want %v", cpForLevel(1.0), cpm[0])
	}
	if got := cpForLevel(50.0); math.Abs(got-cpm[98]) > 1e-12 {
		t.Errorf("cpForLevel(50.0) = %v, want %v (idx 98)", got, cpm[98])
	}
	// Half level 18.0 -> idx 34.
	if got := cpForLevel(18.0); math.Abs(got-cpm[34]) > 1e-12 {
		t.Errorf("cpForLevel(18.0) = %v, want %v (idx 34)", got, cpm[34])
	}
	// Clamp guards.
	if cpForLevel(0.0) != cpm[0] {
		t.Errorf("cpForLevel(0.0) should clamp to cpm[0]")
	}
	if cpForLevel(60.0) != cpm[len(cpm)-1] {
		t.Errorf("cpForLevel(60.0) should clamp to last entry")
	}
}

func TestCPAtFormula(t *testing.T) {
	// pvpoke calculateCP: floor((atk+ivA)*sqrt(def+ivD)*sqrt(hp+ivH)*cpm^2/10)
	p := &Pokemon{BaseStats: BaseStats{Atk: 247, Def: 187, Hp: 162}} // arbitrary
	ivs := IVs{Atk: 15, Def: 15, Hp: 15}
	cpmv := 0.840300023555755
	want := int((262.0 * math.Sqrt(202.0) * math.Sqrt(177.0) * cpmv * cpmv) / 10)
	if got := p.CPAt(cpmv, ivs); got != want {
		t.Errorf("CPAt = %d, want %d", got, want)
	}
}

func TestIvFloorRules(t *testing.T) {
	cases := []struct {
		name     string
		tags     []string
		hasRet   bool
		want     int
	}{
		{"plain", nil, false, 0},
		{"legendary", []string{"legendary"}, false, 1},
		{"shadow legendary", []string{"legendary", "shadow"}, false, 6},
		{"wildlegendary", []string{"legendary", "wildlegendary"}, false, 0},
		{"untradeable", []string{"untradeable"}, false, 10},
		{"return", nil, true, 2},
		{"return + untradeable", []string{"untradeable"}, true, 2}, // RETURN wins (last)
	}
	for _, c := range cases {
		p := &Pokemon{Tags: c.tags}
		if c.hasRet {
			p.FastMoves = []string{"RETURN"}
		}
		if got := p.ivFloor(); got != c.want {
			t.Errorf("%s: ivFloor = %d, want %d", c.name, got, c.want)
		}
	}
}

// TestRankCharizardGreat pins the headline case that was verified end-to-end
// against pvpoke's real JS (via the Python reference): 15/15/15 Charizard at
// 1500 CP ranks #1666 of 4096 at level 18.0, CP 1486.
func TestRankCharizardGreat(t *testing.T) {
	gd := testGameData(t)
	p := lookup(gd, "Charizard")
	if p == nil {
		t.Fatal("Charizard not in game data")
	}
	p.IVs = IVs{15, 15, 15}
	res := p.GetIVRank(SortOverall, 1500)
	if res.Rank != 1666 {
		t.Errorf("rank = %d, want 1666", res.Rank)
	}
	if res.Count != 4096 {
		t.Errorf("count = %d, want 4096", res.Count)
	}
	if math.Abs(res.Level-18.0) > 1e-9 {
		t.Errorf("level = %v, want 18.0", res.Level)
	}
	if res.ActualCP != 1486 {
		t.Errorf("actualCP = %d, want 1486", res.ActualCP)
	}
}

// TestRankLevelFloorSpecies pins species whose baseLevelFloor is non-default
// (the branch that was previously hardcoded to 1.0).
func TestRankLevelFloorSpecies(t *testing.T) {
	gd := testGameData(t)
	cases := []struct {
		name    string
		cp      int
		ivs     IVs
		rank    int
		count   int
	}{
		{"Larvesta", 1500, IVs{15, 15, 15}, 1053, 4096},     // floor 20
		{"Mewtwo (Mega X)", 2500, IVs{15, 15, 15}, 0, 1745}, // floor 15, not reachable
		{"Tauros (Aqua)", 1500, IVs{15, 15, 15}, 0, 1776},   // floor 20, not reachable
		{"Heatran", 2500, IVs{15, 15, 15}, 1936, 3375},     // floor 15, legendary
	}
	for _, c := range cases {
		p := lookup(gd, c.name)
		if p == nil {
			t.Errorf("%s not in game data", c.name)
			continue
		}
		p.IVs = c.ivs
		res := p.GetIVRank(SortOverall, c.cp)
		if res.Rank != c.rank {
			t.Errorf("%s @%d: rank = %d, want %d", c.name, c.cp, res.Rank, c.rank)
		}
		if res.Count != c.count {
			t.Errorf("%s @%d: count = %d, want %d", c.name, c.cp, res.Count, c.count)
		}
	}
}

// TestRankHeatranShadow pins the shadow-legendary floor (6) producing the
// shadow's smaller reachable set.
func TestRankHeatranShadow(t *testing.T) {
	gd := testGameData(t)
	p := lookup(gd, "Heatran (Shadow)")
	if p == nil {
		t.Fatal("Heatran (Shadow) not in game data")
	}
	p.IVs = IVs{15, 15, 15}
	res := p.GetIVRank(SortOverall, 2500)
	if res.Rank != 615 {
		t.Errorf("rank = %d, want 615", res.Rank)
	}
	if res.Count != 1000 {
		t.Errorf("count = %d, want 1000", res.Count)
	}
}

// TestRankVolcarona pins the floating-point associativity case: the overall
// must use (rawHp*atk)*def with the raw (unclamped) hp, otherwise near-ties
// reorder and the rank shifts.
func TestRankVolcarona(t *testing.T) {
	gd := testGameData(t)
	p := lookup(gd, "Volcarona")
	if p == nil {
		t.Fatal("Volcarona not in game data")
	}
	p.IVs = IVs{10, 10, 10}
	res := p.GetIVRank(SortOverall, 2500)
	if res.Rank != 1512 {
		t.Errorf("rank = %d, want 1512 (associativity/overall branch)", res.Rank)
	}
}

// TestPerfectIsRankOne: a perfect-IV Pokémon at a high league should be rank 1.
func TestPerfectIsRankOne(t *testing.T) {
	gd := testGameData(t)
	for _, name := range []string{"Pikachu", "Snorlax"} {
		p := lookup(gd, name)
		if p == nil {
			t.Fatalf("%s not in game data", name)
		}
		p.IVs = IVs{15, 15, 15}
		res := p.GetIVRank(SortOverall, 10000)
		if res.Rank != 1 {
			t.Errorf("%s perfect @10000: rank = %d, want 1", name, res.Rank)
		}
	}
}
