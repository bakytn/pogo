package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Stats holds the three Pokémon GO base stats.
type Stats struct {
	Atk float64 `json:"atk"`
	Def float64 `json:"def"`
	Hp  float64 `json:"hp"`
}

// BaseStats is the baseStats object from a Pokémon entry in gamemaster.json.
type BaseStats struct {
	Atk float64 `json:"atk"`
	Def float64 `json:"def"`
	Hp  float64 `json:"hp"`
}

// Pokemon is the minimal subset of a pvpoke Pokémon entry needed to compute
// battle-independent values (CP, stats, IV rank). Extra fields from the game
// data (moves, tags, family) are kept to make the model extensible.
type Pokemon struct {
	Dex          int         `json:"dex"`
	SpeciesName  string      `json:"speciesName"`
	SpeciesID    string      `json:"speciesId"`
	BaseStats    BaseStats   `json:"baseStats"`
	Types        []string    `json:"types"`
	FastMoves    []string    `json:"fastMoves"`
	ChargedMoves []string    `json:"chargedMoves"`
	Tags         []string    `json:"tags"`
	DefaultIVs   *DefaultIVs `json:"defaultIVs,omitempty"`
	Level25CP    int         `json:"level25CP"`

	// LevelFloor is the minimum level a reachable IV combination may sit at
	// (pvpoke's baseLevelFloor). Most Pokémon are 1; exclusives/legendaries
	// carry 8/15/20 in the game data. Zero means "use 1".
	LevelFloor float64 `json:"levelFloor"`

	// Runtime state.
	CP        int     `json:"-"`
	Level     float64 `json:"-"`
	IVs       IVs     `json:"-"`
	Stats     Stats   `json:"-"`
	MegaLevel int     `json:"-"`
}

// DefaultIVs mirrors the per-CP default IV objects in the game data.
type DefaultIVs struct {
	CP500    []float64 `json:"cp500"`
	CP1500   []float64 `json:"cp1500"`
	CP2500   []float64 `json:"cp2500"`
	CP10000  []float64 `json:"cp10000"`
	CP500L40 []float64 `json:"cp500l40"`
}

// IVs are the individual values for attack, defense and HP (0-15).
type IVs struct {
	Atk int `json:"atk"`
	Def int `json:"def"`
	Hp  int `json:"hp"`
}

// GameData is the loaded game data (a slice of Pokémon plus derived indexes).
type GameData struct {
	Pokemon   []Pokemon `json:"pokemon"`
	Moves     []Attack  `json:"moves"`
	tagIdx    map[string]map[string]bool
	byID      map[string]Pokemon
	byName    map[string]Pokemon
	byNameAll map[string][]int
	byMove    map[string]Attack
}

// NewGameData reads and parses a gamemaster JSON file. It accepts the full
// "gamemaster.json" object (which has a "pokemon" array and a "moves" array)
// or a bare array of Pokémon objects.
func NewGameData(path string) (*GameData, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	gd := &GameData{}
	// Try the wrapped object first.
	var wrapped struct {
		Pokemon []Pokemon `json:"pokemon"`
		Moves   []Attack  `json:"moves"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Pokemon) > 0 {
		gd.Pokemon = wrapped.Pokemon
		gd.Moves = wrapped.Moves
	} else {
		// Bare array.
		if err := json.Unmarshal(raw, &gd.Pokemon); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	gd.buildIndexes()
	return gd, nil
}

func (gd *GameData) buildIndexes() {
	gd.byID = make(map[string]Pokemon, len(gd.Pokemon))
	gd.byName = make(map[string]Pokemon)
	gd.byNameAll = make(map[string][]int)
	for i := range gd.Pokemon {
		p := gd.Pokemon[i]
		gd.Pokemon[i] = p
		gd.byID[p.SpeciesID] = p
		key := strings.ToLower(strings.TrimSpace(p.SpeciesName))
		if key != "" {
			gd.byName[key] = p
			gd.byNameAll[key] = append(gd.byNameAll[key], i)
		}
	}
	if len(gd.Moves) > 0 {
		gd.byMove = make(map[string]Attack, len(gd.Moves))
		for i := range gd.Moves {
			gd.byMove[gd.Moves[i].ID] = gd.Moves[i]
		}
	}
}

// ByID returns the Pokémon with the exact speciesId (form suffix included).
func (gd *GameData) ByID(id string) (Pokemon, bool) {
	p, ok := gd.byID[id]
	return p, ok
}

// ByName returns the best exact-name match (case-insensitive).
func (gd *GameData) ByName(name string) (Pokemon, bool) {
	p, ok := gd.byName[strings.ToLower(strings.TrimSpace(name))]
	return p, ok
}

// Candidate is a matched Pokémon with sort hints.
type Candidate struct {
	P      Pokemon
	Exact  bool
	Prefix bool
	Shadow bool
}

// Candidates returns Pokémon whose name or ID matches the (case-insensitive)
// query as a prefix or substring. Results are sorted: exact names first, then
// prefixes, then other substring matches (shadow forms deprioritized), and
// within each class by name then dex, to keep deterministic ordering.
func (gd *GameData) Candidates(query string) []Candidate {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	seen := make(map[string]bool)
	var out []Candidate
	for i := range gd.Pokemon {
		p := gd.Pokemon[i]
		name := strings.ToLower(p.SpeciesName)
		id := strings.ToLower(p.SpeciesID)
		if !strings.Contains(name, q) && !strings.Contains(id, q) {
			continue
		}
		if seen[p.SpeciesID] {
			continue
		}
		seen[p.SpeciesID] = true
		out = append(out, Candidate{
			P:      p,
			Exact:  name == q,
			Prefix: strings.HasPrefix(name, q) || strings.HasPrefix(id, q),
			Shadow: strings.Contains(p.SpeciesID, "_shadow"),
		})
	}
	sortCandidates(out)
	return out
}

func sortCandidates(c []Candidate) {
	sort.SliceStable(c, func(i, j int) bool {
		a, b := c[i], c[j]
		if a.Exact != b.Exact {
			return a.Exact
		}
		if a.Prefix != b.Prefix {
			return a.Prefix
		}
		if a.Shadow != b.Shadow {
			return !a.Shadow
		}
		if len(a.P.SpeciesName) != len(b.P.SpeciesName) {
			return len(a.P.SpeciesName) < len(b.P.SpeciesName)
		}
		if a.P.SpeciesName != b.P.SpeciesName {
			return a.P.SpeciesName < b.P.SpeciesName
		}
		return a.P.Dex < b.P.Dex
	})
}

// HasTag reports whether the Pokémon carries the given tag.
func (p *Pokemon) HasTag(tag string) bool {
	for _, t := range p.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// HasMove reports whether the Pokémon's charged or fast move pool includes
// the given move ID.
func (p *Pokemon) HasMove(moveID string) bool {
	for _, m := range p.ChargedMoves {
		if m == moveID {
			return true
		}
	}
	for _, m := range p.FastMoves {
		if m == moveID {
			return true
		}
	}
	return false
}

// IsShadow reports whether the species is a shadow form.
func (p *Pokemon) IsShadow() bool {
	return p.HasTag("shadow")
}

// CP computes the CP for the given CPM and IV combination, matching
// pvpoke's Pokemon.calculateCP:
//
//	floor((baseAtk+atkIV) * sqrt(baseDef+defIV) * sqrt(baseHp+hpIV) * cpm^2 / 10)
func (p *Pokemon) CPAt(cpm float64, ivs IVs) int {
	v := float64(p.BaseStats.Atk+float64(ivs.Atk)) *
		math.Sqrt(float64(p.BaseStats.Def+float64(ivs.Def))) *
		math.Sqrt(float64(p.BaseStats.Hp+float64(ivs.Hp))) * cpm * cpm / 10
	return int(v) // truncates toward zero == Math.floor for positive values
}

// SetStats applies the CP + level + IVs to the runtime stats, mirroring
// pvpoke's maximizeStat stat assignment.
func (p *Pokemon) SetStats(cpm float64, ivs IVs) Stats {
	st := Stats{
		Atk: cpm * (p.BaseStats.Atk + float64(ivs.Atk)),
		Def: cpm * (p.BaseStats.Def + float64(ivs.Def)),
	}
	hp := int64(cpm * (p.BaseStats.Hp + float64(ivs.Hp)))
	if hp < 10 {
		hp = 10
	}
	st.Hp = float64(hp)
	return st
}

// Move returns the move table entry for a move id.
func (gd *GameData) Move(id string) (Attack, bool) {
	m, ok := gd.byMove[id]
	return m, ok
}

// AllMoves returns every move whose id is in the list (deduped, in list order).
func (gd *GameData) AllMoves(ids []string) []Attack {
	out := make([]Attack, 0, len(ids))
	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			continue
		}
		m, ok := gd.byMove[id]
		if !ok {
			continue
		}
		seen[id] = true
		out = append(out, m)
	}
	return out
}

// FastMoves returns the Pokémon's fast moves as Attack values.
func (p *Pokemon) FastMovesGD(gd *GameData) []Attack {
	return gd.AllMoves(p.FastMoves)
}

// ChargedMoves returns the Pokémon's charged moves as Attack values.
func (p *Pokemon) ChargedMovesGD(gd *GameData) []Attack {
	return gd.AllMoves(p.ChargedMoves)
}

// MaxStats returns the maximized battle stats at the level for the given
// CPM: the same multiply-out as SetStats, but without any IV — the "all IVs
// are 15" / extreme case. pvpoke's calculateBreakpoints uses the maximum
// generateIVCombinations value, which for a fully-maxed IV floor is exactly
// cpm*(base+15) for atk/def and floor(cpm*(base+15)) for hp.
func (p *Pokemon) MaxStats(cpm float64, shadow bool) Stats {
	iv := IVs{Atk: 15, Def: 15, Hp: 15}
	st := p.SetStats(cpm, iv)
	if shadow {
		st.Atk *= DmgShadowAtk
		st.Def *= DmgShadowDef
	}
	return st
}

// DefaultStats returns the battle stats for the Pokémon in its "gamemaster
// default" state at the given league CP, mirroring pvpoke's
// Pokemon.initialize(true) "gamemaster" branch (the state the published
// rankings are built from). It is SetStats over (IVs, level) from
// DefaultCombo, with shadow multipliers folded in when shadow is set.
func (p *Pokemon) DefaultStats(targetCP int, shadow bool) Stats {
	ivs, level := p.DefaultCombo(targetCP)
	st := p.SetStats(cpForLevel(level), ivs)
	if shadow {
		st.Atk *= DmgShadowAtk
		st.Def *= DmgShadowDef
	}
	return st
}

// DefaultCombo returns the IVs and level of the Pokémon in its "gamemaster
// default" state at the given league CP, mirroring pvpoke's
// Pokemon.initialize(true) "gamemaster" branch (the state the published
// rankings are built from):
//
//   - CP < 10000: read the per-CP row of the game-data defaultIVs table
//     ([level, atkIV, defIV, hpIV]); the level is clamped to the 50 cap.
//   - master (10000): there is no table row, so use 15/15/15 at level 50.
//   - a missing/empty row falls back to 15/15/15 at level 50 (pvpoke's own
//     fallback when no valid combination exists).
func (p *Pokemon) DefaultCombo(targetCP int) (IVs, float64) {
	ivs := IVs{Atk: 15, Def: 15, Hp: 15}
	level := 50.0
	if targetCP != 10000 {
		if c := p.defaultIVCombo(targetCP); c != nil && len(c) == 4 {
			level = math.Min(50.0, c[0])
			ivs = IVs{Atk: int(c[1]), Def: int(c[2]), Hp: int(c[3])}
		}
	}
	return ivs, level
}

// defaultIVCombo returns the per-CP row of the defaultIVs table for the given
// league CP: [level, atkIV, defIV, hpIV]. Returns nil when the row is absent
// (e.g. no cp500 entry for a species that can't appear in little cup).
func (p *Pokemon) defaultIVCombo(targetCP int) []float64 {
	if p.DefaultIVs == nil {
		return nil
	}
	switch targetCP {
	case 500:
		return p.DefaultIVs.CP500
	case 1500:
		return p.DefaultIVs.CP1500
	case 2500:
		return p.DefaultIVs.CP2500
	}
	return nil
}

// DefaultDataDir returns the default location of game data relative to CWD.
func DefaultDataDir() string { return "data" }

func DataPath(dir, filename string) string { return filepath.Join(dir, filename) }
