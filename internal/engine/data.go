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
	Dex         int      `json:"dex"`
	SpeciesName string   `json:"speciesName"`
	SpeciesID   string   `json:"speciesId"`
	BaseStats   BaseStats `json:"baseStats"`
	Types       []string `json:"types"`
	FastMoves   []string `json:"fastMoves"`
	ChargedMoves []string `json:"chargedMoves"`
	Tags        []string `json:"tags"`
	DefaultIVs  *DefaultIVs `json:"defaultIVs,omitempty"`
	Level25CP   int        `json:"level25CP"`

	// LevelFloor is the minimum level a reachable IV combination may sit at
	// (pvpoke's baseLevelFloor). Most Pokémon are 1; exclusives/legendaries
	// carry 8/15/20 in the game data. Zero means "use 1".
	LevelFloor float64 `json:"levelFloor"`

	// Runtime state.
	CP         int       `json:"-"`
	Level      float64   `json:"-"`
	IVs        IVs       `json:"-"`
	Stats      Stats     `json:"-"`
	MegaLevel  int       `json:"-"`
}

// DefaultIVs mirrors the per-CP default IV objects in the game data.
type DefaultIVs struct {
	CP500   []float64 `json:"cp500"`
	CP1500  []float64 `json:"cp1500"`
	CP2500  []float64 `json:"cp2500"`
	CP10000 []float64 `json:"cp10000"`
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
	Pokemon   []Pokemon  `json:"pokemon"`
	movesIdx  map[string]bool
	tagIdx    map[string]map[string]bool
	byID      map[string]Pokemon
	byName    map[string]Pokemon
	byNameAll map[string][]int
}

// NewGameData reads and parses a gamemaster JSON file. It accepts the full
// "gamemaster.json" object (which has a "pokemon" array) or a bare array of
// Pokémon objects.
func NewGameData(path string) (*GameData, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	gd := &GameData{}
	// Try the wrapped object first.
	var wrapped struct {
		Pokemon []Pokemon `json:"pokemon"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Pokemon) > 0 {
		gd.Pokemon = wrapped.Pokemon
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

// DefaultDataDir returns the default location of game data relative to CWD.
func DefaultDataDir() string { return "data" }

func DataPath(dir, filename string) string { return filepath.Join(dir, filename) }
