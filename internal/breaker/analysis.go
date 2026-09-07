package breaker

import (
	"strings"

	"github.com/bakytn/pogo/internal/engine"
)

// State names for the four IV evaluation states, in display order.
const (
	StateNow    = "now"
	StateRank1  = "rank 1"
	StateMaxAtk = "atk 15-0-0"
	StateMaxDef = "def 0-15-0"
)

// stateOrder is the canonical order of the IV states (and of the damage
// values stored in MoveDamage.States).
var stateOrder = []string{StateNow, StateRank1, StateMaxAtk, StateMaxDef}

// State is one evaluation of the user's Pokémon in the league.
type State struct {
	Name string
	Bs   engine.BattleState
}

// ExtremeAtk returns the IV combo that maximizes attack: 15 atk, floor
// def/hp (the floor honors shadow/legendary/untradeable rules).
func ExtremeAtk(p engine.Pokemon) engine.IVs {
	f := p.IVFloor()
	return engine.IVs{Atk: 15, Def: f, Hp: f}
}

// ExtremeDef returns the IV combo that maximizes defense: 15 def, floor
// atk/hp.
func ExtremeDef(p engine.Pokemon) engine.IVs {
	f := p.IVFloor()
	return engine.IVs{Atk: f, Def: 15, Hp: f}
}

// Rank1IVs returns the IV combo with the highest stat product reachable in the
// league (a fresh "Rank 1"), by ranking the reachable combinations and taking
// the top. Falls back to 15/15/15 when nothing is reachable.
func Rank1IVs(p engine.Pokemon, cp int) engine.IVs {
	combos := p.GenerateIVCombinations(engine.SortOverall, cp)
	if len(combos) == 0 {
		return engine.IVs{Atk: 15, Def: 15, Hp: 15}
	}
	return combos[0].IVs
}

// UserStates resolves the four IV states (now, rank 1, max-atk, max-def) into
// battle-ready stats for the user's Pokémon in the league. Shadow is applied
// consistently across all four states so the deltas isolate the IV effect.
func UserStates(p engine.Pokemon, userIVs engine.IVs, cp int, shadow bool) map[string]State {
	stNow := p.BattleState(userIVs, cp, shadow)
	stRank := p.BattleState(Rank1IVs(p, cp), cp, shadow)
	stAtk := p.BattleState(ExtremeAtk(p), cp, shadow)
	stDef := p.BattleState(ExtremeDef(p), cp, shadow)
	return map[string]State{
		StateNow:    {StateNow, stNow},
		StateRank1:  {StateRank1, stRank},
		StateMaxAtk: {StateMaxAtk, stAtk},
		StateMaxDef: {StateMaxDef, stDef},
	}
}

// MoveDamage is one move's damage against one opponent across the IV states.
type MoveDamage struct {
	Move     engine.Attack
	Fast     bool
	States   []int // damage in stateOrder order: now, rank1, maxAtk, maxDef
	Delta    int   // States[rank1] - States[now]  (what Rank 1 "gives" vs your IVs)
	MaxDelta int   // max(states) - States[now]    (the best any extreme gives)
}

// Opponent is one top meta Pokémon the user is broken down against. It is
// fixed at its rank-1 stats (from the meta file) plus shadow multipliers.
type Opponent struct {
	Entry  TopMetaEntry
	Types  []string
	Shadow bool
	Atk    float64 // effective atk (shadow-adjusted)
	Def    float64 // effective def (shadow-adjusted)
	Moves  []MoveDamage
}

// Analyzer holds the inputs to a single breaker analysis and caches the user's
// IV states. Build it via NewAnalyzer, then call Analyze.
type Analyzer struct {
	gd     *engine.GameData
	Poke   engine.Pokemon
	User   engine.IVs
	Shadow bool
	CP     int
	states map[string]State
}

// NewAnalyzer resolves the user's Pokémon (by id or name) and its four IV
// states for the league.
func NewAnalyzer(gd *engine.GameData, name string, userIVs engine.IVs, cp int, shadow bool) (*Analyzer, error) {
	poke, ok := gd.ByID(name)
	if !ok {
		if cands := gd.Candidates(name); len(cands) == 1 {
			poke = cands[0].P
		} else {
			return nil, errNoMatch(name, cands)
		}
	}
	a := &Analyzer{gd: gd, Poke: poke, User: userIVs, Shadow: shadow, CP: cp}
	a.states = UserStates(poke, userIVs, cp, shadow)
	return a, nil
}

func errNoMatch(name string, cands []engine.Candidate) error {
	if len(cands) == 0 {
		return &ErrNoMatch{Name: name}
	}
	var parts []string
	for i := range cands {
		parts = append(parts, cands[i].P.SpeciesName)
	}
	if len(parts) > 5 {
		parts = parts[:5]
	}
	return &ErrNoMatch{Name: name, Matches: parts}
}

// ErrNoMatch reports a Pokémon lookup that produced no (or several) matches.
type ErrNoMatch struct {
	Name    string
	Matches []string
}

func (e *ErrNoMatch) Error() string {
	if len(e.Matches) == 0 {
		return "no Pokémon matches " + e.Name
	}
	return "ambiguous Pokémon " + e.Name + " — " + strings.Join(e.Matches, ", ")
}

// baseID strips a shadow/mega form suffix from a speciesId to get the base
// species (so a shadow form's types resolve from the base entry).
func baseID(id string) string {
	i := strings.LastIndex(id, "_")
	if i > 0 {
		return id[:i]
	}
	return id
}

// isShadow reports whether a speciesId is a shadow form.
func isShadowID(id string) bool {
	l := strings.ToLower(id)
	return strings.Contains(l, "_shadow") || strings.HasPrefix(l, "shadow_")
}

// Analyze evaluates every opponent in the league's top list and returns the
// populated opponents (with per-move damage across the IV states).
func (a *Analyzer) Analyze(league *League) []*Opponent {
	out := make([]*Opponent, 0, len(league.Top))
	for i := range league.Top {
		e := league.Top[i]
		out = append(out, a.analyzeOpponent(&e))
	}
	return out
}

// analyzeOpponent fixes the opponent at its rank-1 stats and computes each of
// the user's moves' damage across the IV states.
func (a *Analyzer) analyzeOpponent(e *TopMetaEntry) *Opponent {
	opp := &Opponent{Entry: *e}

	// Opponent types: resolve from the speciesId (base form for shadow).
	id := e.SpeciesID
	opp.Shadow = isShadowID(id)
	if poke, ok := a.gd.ByID(id); ok {
		opp.Types = poke.Types
	} else if poke, ok := a.gd.ByID(baseID(id)); ok {
		opp.Types = poke.Types
	} else if cands := a.gd.Candidates(e.SpeciesName); len(cands) == 1 {
		opp.Types = cands[0].P.Types
	}

	// Fixed at its rank-1 (max stat product) stats from the meta file; shadow
	// forms get the site's atk/def multipliers.
	opp.Atk = e.Stats.Atk
	opp.Def = e.Stats.Def
	if opp.Shadow {
		opp.Atk *= engine.DmgShadowAtk
		opp.Def *= engine.DmgShadowDef
	}

	userTypes := a.Poke.Types

	// The user's moves (their pool), each evaluated against this opponent.
	userFast := a.gd.AllMoves(a.Poke.FastMoves)
	userCharged := a.gd.AllMoves(a.Poke.ChargedMoves)

	addMove := func(m engine.Attack, isFast bool) {
		m.Stab = engine.Stab(m.Type, userTypes)
		eff := engine.Effectiveness(m.Type, opp.Types)
		states := make([]int, 0, len(stateOrder))
		for _, name := range stateOrder {
			st := a.states[name]
			dmg := engine.Damage(st.Bs.Atk, opp.Def, m.Power, m.Stab, eff)
			states = append(states, dmg)
		}
		md := MoveDamage{
			Move:   m,
			Fast:   isFast,
			States: states,
			Delta:  states[idx(StateRank1)] - states[idx(StateNow)],
		}
		md.MaxDelta = max(states) - states[idx(StateNow)]
		opp.Moves = append(opp.Moves, md)
	}
	for _, m := range userFast {
		addMove(m, true)
	}
	for _, m := range userCharged {
		addMove(m, false)
	}
	return opp
}

func idx(name string) int {
	for i, s := range stateOrder {
		if s == name {
			return i
		}
	}
	return 0
}

// max returns the largest int in the slice.
func max(vals []int) int {
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}
