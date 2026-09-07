// Package breaker implements the `breaker` command: for a user's Pokémon
// (species + IVs + league) it reports, against each of the top meta Pokémon
// of that league, the damage its moves deal and how that damage changes when
// the Pokémon's IVs are pushed to the four relevant extremes:
//
//   - "now"        the user's actual IVs (their actual battle stats)
//   - "rank 1"     the highest stat product reachable in the league
//   - "atk 15-0-0" max attack / floor defense / floor HP
//   - "def 0-15-0" floor attack / max defense / floor HP
//
// The damage math is a faithful port of pvpoke's DamageCalculator
// (see internal/engine/damage.go); the top meta lists come from pvpoke's
// published rankings (data/meta/<cp>.json).
package breaker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/bakytn/pogo/internal/engine"
)

// TopMetaEntry is one opponent from a data/meta/<cp>.json file: the species,
// the meta moveset it runs, and its rank-1 (max stat product) battle stats.
type TopMetaEntry struct {
	SpeciesID    string       `json:"speciesId"`
	SpeciesName  string       `json:"speciesName"`
	Score        float64      `json:"score"`
	FastMoves    []string     `json:"fastMoves"`
	ChargedMoves []string     `json:"chargedMoves"`
	Stats        engine.Stats `json:"stats"`
}

// League is a loaded per-league top meta file.
type League struct {
	League string         `json:"league"` // little / great / ultra / master
	CP     int            `json:"cp"`     // 500 / 1500 / 2500 / 10000
	Top    []TopMetaEntry `json:"top"`
}

// CPForLeague maps league names to their CP ceilings.
var CPForLeague = map[string]int{
	"little": 500,
	"great":  1500,
	"ultra":  2500,
	"master": 10000,
}

// LeagueName maps a CP ceiling to its common name.
func LeagueName(cp int) string {
	switch {
	case cp <= 500:
		return "little"
	case cp <= 1500:
		return "great"
	case cp <= 2500:
		return "ultra"
	default:
		return "master"
	}
}

// ParseLeague resolves a league argument (a name or a raw CP) to its CP
// ceiling. Returns 0 when it does not map to a standard league.
func ParseLeague(league string) int {
	s := strings.ToLower(strings.TrimSpace(league))
	if cp, ok := CPForLeague[s]; ok {
		return cp
	}
	if v, err := strconv.Atoi(s); err == nil {
		if _, ok := CPForLeague[LeagueName(v)]; ok {
			return v
		}
	}
	return 0
}

// Loader lazily reads the static meta files out of a directory (the
// data/meta/<cp>.json layout), caching them per CP.
type Loader struct {
	dir    string
	mu     sync.Mutex
	cached map[int]*League
}

// NewLoader builds a Loader rooted at dir. When dir is empty, DefaultMetaDir.
func NewLoader(dir string) *Loader {
	if dir == "" {
		dir = DefaultMetaDir()
	}
	return &Loader{dir: dir, cached: make(map[int]*League)}
}

// DefaultMetaDir is the standard location of the meta files relative to CWD.
func DefaultMetaDir() string { return filepath.Join("data", "meta") }

// Load reads the top meta list for a league (by name or CP), caching it.
func (l *Loader) Load(league string) (*League, error) {
	cp := ParseLeague(league)
	if cp == 0 {
		return nil, fmt.Errorf("unknown league %q (expected little / great / ultra / master, or 500 / 1500 / 2500 / 10000)", league)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if lg, ok := l.cached[cp]; ok {
		return lg, nil
	}
	path := filepath.Join(l.dir, fmt.Sprintf("%d.json", cp))
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("meta file for %d CP not found: %w (looked for %s)", cp, err, path)
	}
	var lg League
	if err := json.Unmarshal(raw, &lg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	lg.CP = cp
	lg.League = LeagueName(cp)
	l.cached[cp] = &lg
	return &lg, nil
}
