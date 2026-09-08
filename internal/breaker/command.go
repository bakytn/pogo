package breaker

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bakytn/pogo/internal/engine"
)

// Request carries the breaker inputs.
type Request struct {
	Name   string
	IVs    engine.IVs
	CP     int
	Shadow bool
	League string // "little"/"great"/"ultra"/"master" or a CP value
	Dump   bool   // emit machine-readable lines instead of the human report
}

// Command is the `breaker` command.
type Command struct{}

// NewCommand returns a breaker command.
func NewCommand() *Command { return &Command{} }

// Name is the user-facing token.
func (c *Command) Name() string { return "breaker" }

// Description is a one-line help string.
func (c *Command) Description() string {
	return "Breakpoints: how your moves' damage changes vs the top meta at Rank 1 / 15-0-0 / 0-15-0."
}

// Run executes the breaker analysis and returns a rendered report.
func (c *Command) Run(gd *engine.GameData, loader *Loader, req Request) (string, error) {
	if req.Name == "" {
		return "", fmt.Errorf("usage: breaker <pokemon> [cp] [atk def hp] [league]")
	}
	cp := req.CP
	if cp == 0 {
		cp = 1500
	}
	// The league file to load is the one the user named, else the league of cp.
	leagueArg := req.League
	if leagueArg == "" {
		leagueArg = LeagueName(cp)
	}
	// Keep the analysis CP consistent with the league the user named.
	if v := ParseLeague(leagueArg); v > 0 {
		cp = v
	}

	league, err := loader.Load(leagueArg)
	if err != nil {
		return "", err
	}

	a, err := NewAnalyzer(gd, req.Name, req.IVs, cp, req.Shadow)
	if err != nil {
		return "", err
	}
	opponents := a.Analyze(league)

	var b strings.Builder
	if req.Dump {
		writeDump(&b, a, league, opponents)
	} else {
		writeReport(&b, a, league, opponents)
	}
	return b.String(), nil
}

// writeDump emits a machine-readable, line-oriented form of the report for
// exact diffing against the Python oracle (bp_ref.py). Format:
//
//	STATES <nowA rank1A maxatkA maxdefA> <nowD rank1D maxatkD maxdefD>
//	    the user's four IV states' full-precision atk and def, in stateOrder.
//	OPP <rank> <shadow 0/1> <oppSpeciesId>
//	<oppRank> <shadow 0/1> <moveId> <fast 0/1> <now> <rank1> <maxatk> <maxdef>
//	    one line per user (break) move: damage dealt to the meta per state.
//	<oppRank> <shadow 0/1> BULK <moveId> <now> <rank1> <maxatk> <maxdef>
//	    the meta's fast move hitting the user per state (the bulkpoint side).
func writeDump(b *strings.Builder, a *Analyzer, league *League, opponents []*Opponent) {
	// The four canonical IV states' attack and defense values at full
	// precision, so the oracle can validate both the break (user atk → opp
	// def) and bulkpoint (opp atk → user def) damage math independently of
	// re-implementing the CP walk (rank1 / level selection is already validated
	// by pvpoke_ref.py).
	var atkVals, defVals []float64
	for _, name := range stateOrder {
		obs := a.states[name].Bs
		atkVals = append(atkVals, obs.Atk)
		defVals = append(defVals, obs.Def)
	}
	sv := make([]string, 0, len(atkVals)+len(defVals))
	for _, v := range atkVals {
		sv = append(sv, strconv.FormatFloat(v, 'g', -1, 64)) // shortest round-trippable form
	}
	for _, v := range defVals {
		sv = append(sv, strconv.FormatFloat(v, 'g', -1, 64))
	}
	fmt.Fprintf(b, "STATES %s\n", strings.Join(sv, " "))
	for i, o := range opponents {
		sh := 0
		if o.Shadow {
			sh = 1
		}
		fmt.Fprintf(b, "OPP %d %d %s\n", i+1, sh, o.Entry.SpeciesID)
		// Break side: the user's moves.
		for _, m := range o.Moves {
			fast := 0
			if m.Fast {
				fast = 1
			}
			s := m.States
			fmt.Fprintf(b, "%d %d %s %d %d %d %d %d\n",
				i+1, sh, m.Move.ID, fast,
				s[idx(StateNow)], s[idx(StateRank1)], s[idx(StateMaxAtk)], s[idx(StateMaxDef)])
		}
		// Bulkpoint side: the meta's fast move hitting the user.
		for _, m := range o.Bulk {
			s := m.States
			fmt.Fprintf(b, "%d %d BULK %s %d %d %d %d\n",
				i+1, sh, m.Move.ID,
				s[idx(StateNow)], s[idx(StateRank1)], s[idx(StateMaxAtk)], s[idx(StateMaxDef)])
		}
	}
}

func fmtInt(ivs engine.IVs) string {
	return fmt.Sprintf("%d/%d/%d", ivs.Atk, ivs.Def, ivs.Hp)
}

func writeReport(b *strings.Builder, a *Analyzer, league *League, opponents []*Opponent) {
	title := a.Poke.SpeciesName
	if t := engine.FormatTypes(a.Poke.Types); t != "" {
		title += " (" + t + ")"
	}
	fmt.Fprintf(b, "%s vs **%s** (%d CP) · IVs %s\n", title, LeagueName(league.CP), league.CP, fmtInt(a.User))
	if a.Shadow {
		b.WriteString("_shadow form_\n")
	}

	// The four IV states, so the reader can map the damage tuples.
	b.WriteString("Your stats: ")
	first := true
	for _, name := range stateOrder {
		st := a.states[name]
		s := fmt.Sprintf("%s A%.0f D%.0f H%.0f", name, st.Bs.Atk, st.Bs.Def, st.Bs.Hp)
		if !first {
			b.WriteString(" · ")
		}
		b.WriteString(s)
		first = false
	}
	b.WriteString("\n\n")
	b.WriteString("damage = now / Rank 1 / 15-0-0 / 0-15-0  (your 4 Add-&-Compare IV states)\n")
	b.WriteString("⚡ = your move hitting the meta (break) · 🛡 = meta's move hitting you (bulk)\n")
	b.WriteString("meta fights at its default IVs\n\n")

	for i, o := range opponents {
		if i > 0 {
			b.WriteString("\n")
		}
		label := o.Entry.SpeciesName
		if o.Shadow {
			label += " (shadow)"
		}
		fmt.Fprintf(b, "%d. %s%s%s\n", i+1, label, typeLine(o.Types), oppDefaults(o))
		for _, m := range o.Moves {
			name := m.Move.Name
			if name == "" {
				name = titleCase(m.Move.ID)
			}
			tag := "  "
			if m.Fast {
				tag = "⚡"
			}
			st := m.States
			line := fmt.Sprintf("%s%s %2d / %2d / %2d / %2d", tag, pad(name, 14), st[idx(StateNow)], st[idx(StateRank1)], st[idx(StateMaxAtk)], st[idx(StateMaxDef)])
			d := st[idx(StateRank1)] - st[idx(StateNow)]
			if d != 0 {
				line += fmt.Sprintf("  (R1 %s%d)", sign(d), abs(d))
			}
			if md := max(st) - st[idx(StateNow)]; md > d {
				line += fmt.Sprintf("  best +%d", md)
			}
			b.WriteString(line + "\n")
		}
		for _, m := range o.Bulk {
			name := m.Move.Name
			if name == "" {
				name = titleCase(m.Move.ID)
			}
			st := m.States
			line := fmt.Sprintf("🛡 %s %2d / %2d / %2d / %2d", pad(name, 12), st[idx(StateNow)], st[idx(StateRank1)], st[idx(StateMaxAtk)], st[idx(StateMaxDef)])
			// Larger = the meta hits you harder. Highlight the jump to your
			// thinnest (lowest-def) state vs your current state.
			if md := max(st) - st[idx(StateNow)]; md > 0 {
				line += fmt.Sprintf("  worst +%d", md)
			}
			b.WriteString(line + "\n")
		}
	}
}

// oppDefaults renders the meta's default-IV line for the report (the IVs and
// level it fights at, per the defaultIVs table).
func oppDefaults(o *Opponent) string {
	iv := fmt.Sprintf(" (IVs %d/%d/%d", o.IVs.Atk, o.IVs.Def, o.IVs.Hp)
	if o.Level > 0 {
		iv += fmt.Sprintf(" · Lv%.1f", o.Level)
	}
	return iv + ")"
}

func typeLine(types []string) string {
	if len(types) == 0 {
		return ""
	}
	return " " + strings.ToLower(strings.Join(types, "/"))
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func sign(v int) string {
	if v >= 0 {
		return "+"
	}
	return ""
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}
