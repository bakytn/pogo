package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/bakytn/pogo/internal/engine"
)

// Request is the normalized input to a command. Handlers use whatever fields
// they need; unknown fields are simply ignored, which keeps the type stable
// while new commands are added.
type Request struct {
	// Name is the Pokémon name or ID the user supplied (already trimmed).
	Name string

	// IVs is the requested individual values. When nil, the handler should
	// default to 15/15/15 (a perfect IV Pokémon).
	IVs *engine.IVs

	// CP is the league / league-CP to evaluate against.
	CP int

	// SortStat controls which stat the IV rank is sorted by.
	SortStat engine.SortStat

	// Shadow flags the user asked for the shadow form of a species.
	Shadow bool

	// League is the named league for commands that operate against a league's
	// top meta (e.g. `breaker`): little / great / ultra / master, or a CP.
	League string

	// Extra carries arbitrary named values for commands that need more than
	// the common fields above.
	Extra map[string]string
}

// Command is the extension point: any feature the bot exposes (rank, stat
// lookup, moveset, counter, damage calc, ...) implements this interface and
// registers itself with the Service. Both the CLI and the Discord bot iterate
// over the registered commands, so adding a capability is a single Register
// call that is picked up everywhere at once.
type Command interface {
	// Name is the user-facing command token (lowercase).
	Name() string
	// Description is a one-line help string.
	Description() string
	// Run executes the command and returns a human-readable reply.
	Run(ctx context.Context, svc *Service, req Request) (string, error)
}

// Service holds shared game state and the registry of commands.
type Service struct {
	Data     *engine.GameData
	commands map[string]Command
	order    []string
}

// New builds a Service around the provided game data and seeds it with the
// built-in commands.
func New(gd *engine.GameData) *Service {
	s := &Service{
		Data:     gd,
		commands: make(map[string]Command),
	}
	s.Register(NewRankCommand())
	return s
}

// Register adds a command. If a command with the same name already exists it
// is replaced.
func (s *Service) Register(c Command) {
	if _, exists := s.commands[c.Name()]; !exists {
		s.order = append(s.order, c.Name())
	}
	s.commands[c.Name()] = c
}

// Command returns a registered command by name.
func (s *Service) Command(name string) (Command, bool) {
	c, ok := s.commands[strings.ToLower(strings.TrimSpace(name))]
	return c, ok
}

// Commands returns the registered command names in registration order.
func (s *Service) Commands() []string {
	return append([]string(nil), s.order...)
}

// Help renders a short help listing.
func (s *Service) Help() string {
	var b strings.Builder
	b.WriteString("Available commands:\n")
	for _, name := range s.order {
		b.WriteString(fmt.Sprintf("  %s — %s\n", name, s.commands[name].Description()))
	}
	return b.String()
}

// Resolve finds the Pokémon for a request, handling name normalization, shadow
// forms, and ambiguous matches. It returns the resolved Pokémon and a note to
// show the user (empty on a clean single match).
func (s *Service) Resolve(req Request) (engine.Pokemon, string, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return engine.Pokemon{}, "", fmt.Errorf("no Pokémon name given")
	}

	cands := s.Data.Candidates(name)
	if len(cands) == 0 {
		return engine.Pokemon{}, "", fmt.Errorf("no Pokémon matches %q", name)
	}
	if len(cands) == 1 {
		return cands[0].P, "", nil
	}

	// Multiple matches: apply the shadow filter when the user requested one.
	var shadowCands []engine.Candidate
	for _, c := range cands {
		isShadow := c.Shadow
		if req.Shadow && isShadow {
			shadowCands = append(shadowCands, c)
		}
		if !req.Shadow && !isShadow {
			shadowCands = append(shadowCands, c)
		}
	}
	if len(shadowCands) == 1 {
		return shadowCands[0].P, "", nil
	}
	if len(shadowCands) > 1 {
		cands = shadowCands
	}

	// Still ambiguous: return the top candidate but tell the user.
	names := make([]string, 0, len(cands))
	for _, c := range cands {
		names = append(names, c.P.SpeciesName)
	}
	sort.Strings(names)
	n := minInt(len(names), 5)
	return cands[0].P, fmt.Sprintf("matched %q (also: %s)", cands[0].P.SpeciesName, strings.Join(names[:n], ", ")), nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// defaultIVs returns the request's IVs or 15/15/15.
func (s *Service) defaultIVs(req Request) engine.IVs {
	if req.IVs != nil {
		return *req.IVs
	}
	return engine.IVs{Atk: 15, Def: 15, Hp: 15}
}
