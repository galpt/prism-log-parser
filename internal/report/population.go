// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package report

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// The line shapes below come from the cell_text printer in montauk's
// prom_population.cpp. Only the forms that prism-frame can trigger are
// accepted, because it passes neither --relative-to nor --full. Names are
// printed lowercase and may contain spaces, even " vs " or "=", so no pattern
// can tell where a name ends. The lines are cut by matching names against the
// schedulers of this log's own tables, and the patterns below only describe
// the text after the names.
var (
	pairTailRE = regexp.MustCompile(`^ \[N=(\d+)/(\d+)\]: ` +
		`mean (\S+) \(([^)]*)\) vs (\S+) \(([^)]*)\) \| ` +
		`cliff (\S+) \((\S+)\) \| perm p=(\S+) \| power (\d+|>20)$`)
	onlyTailRE = regexp.MustCompile(`^ \[N=(\d+)\]: mean (\S+) \(([^)]*)\) -- no comparison$`)
	mcbRE      = regexp.MustCompile(`^MCB \((higher|lower)=better\): (.+)$`)
)

const onlyNoneLine = "only (none) -- no comparison"

// populationHeader expects the POPULATION line that prism-frame writes before
// pasting montauk's output.
func populationHeader(p *parser, line string) (stateFn, error) {
	if line != populationLine {
		return nil, p.fail("expected %q", populationLine)
	}
	p.inPopulation = true
	p.known = p.schedulerNames()
	return population, nil
}

// population handles montauk's text. The indentation tells the line kinds
// apart: METRIC at column 0, the "[labels]" cell header at 2 spaces, and the
// pair, only and MCB lines at 4. A blank line ends the current METRIC block.
// A paragraph that does not open with METRIC is montauk's stderr text, which
// prism-frame pastes when montauk prints no comparison, so it is copied as is.
func population(p *parser, line string) (stateFn, error) {
	if p.freeText {
		if line == "" {
			p.freeText = false
		} else {
			p.rep.PopulationText = append(p.rep.PopulationText, line)
		}
		return population, nil
	}
	var err error
	switch {
	case line == "":
		p.famOpen, p.grpOpen = false, false
	case strings.HasPrefix(line, "METRIC "):
		err = p.startFamily(line[len("METRIC "):])
	case !p.famOpen:
		p.freeText = true
		p.rep.PopulationText = append(p.rep.PopulationText, line)
	case strings.HasPrefix(line, "    "):
		err = p.cellLine(line[4:])
	case strings.HasPrefix(line, "  "):
		err = p.startGroup(line[2:])
	default:
		err = p.fail("unknown line in the POPULATION section")
	}
	if err != nil {
		return nil, err
	}
	return population, nil
}

func (p *parser) family() *Family {
	return &p.rep.Population[len(p.rep.Population)-1]
}

func (p *parser) group() *Group {
	f := p.family()
	return &f.Groups[len(f.Groups)-1]
}

func (p *parser) startFamily(name string) error {
	if name == "" || strings.ContainsFunc(name, unicode.IsSpace) {
		return p.fail("bad family name")
	}
	f := Family{Name: name}
	if key, ok := strings.CutPrefix(name, "pandemonium_frame_higher_"); ok {
		f.Key, f.Direction = key, HigherIsBetter
	} else if key, ok := strings.CutPrefix(name, "pandemonium_frame_lower_"); ok {
		f.Key, f.Direction = key, LowerIsBetter
	}
	p.rep.Population = append(p.rep.Population, f)
	p.famOpen, p.grpOpen = true, false
	return nil
}

func (p *parser) startGroup(text string) error {
	body, ok := strings.CutPrefix(text, "[")
	body, closed := strings.CutSuffix(body, "]")
	if !ok || !closed {
		return p.fail("expected a \"[labels]\" cell header inside a METRIC block")
	}
	labels := map[string]string{}
	if body != "(no labels)" {
		for _, kv := range strings.Fields(body) {
			k, v, found := strings.Cut(kv, "=")
			if _, dup := labels[k]; !found || k == "" || dup {
				return p.fail("bad label %q in the cell header", kv)
			}
			labels[k] = v
		}
	}
	f := p.family()
	f.Groups = append(f.Groups, Group{Labels: labels})
	p.grpOpen = true
	return nil
}

// cellLine handles a pair, an only or an MCB line, which all belong to the
// most recent "[labels]" header.
func (p *parser) cellLine(text string) error {
	if !p.grpOpen {
		return p.fail("population line outside a \"[labels]\" cell")
	}
	g := p.group()
	if pairs := p.splitPairs(text); len(pairs) > 1 {
		return p.fail("pair line splits into two schedulers in more than one way")
	} else if len(pairs) == 1 {
		return p.addPair(g, pairs[0])
	}
	if rest, ok := strings.CutPrefix(text, "only "); ok {
		if only := p.splitOnly(rest); len(only) > 1 {
			return p.fail("only line names a scheduler in more than one way")
		} else if len(only) == 1 {
			return p.addOnly(g, only[0])
		}
	}
	if text == onlyNoneLine {
		g.Only = append(g.Only, Only{})
		return nil
	}
	if m := mcbRE.FindStringSubmatch(text); m != nil {
		return p.addStandings(g, m)
	}
	return p.fail("unknown line in the POPULATION section, or a scheduler it names is not in the tables")
}

// schedulerNames returns the schedulers of every table in the log, lowercased
// as montauk prints them. Longer names come first so that a name never loses
// to a shorter name that is its prefix.
func (p *parser) schedulerNames() []string {
	seen := map[string]bool{}
	var names []string
	for _, sc := range p.rep.Scenarios {
		for _, n := range sc.Schedulers {
			if lower := strings.ToLower(n); !seen[lower] {
				seen[lower] = true
				names = append(names, lower)
			}
		}
	}
	slices.SortFunc(names, func(a, b string) int {
		return cmp.Or(cmp.Compare(len(b), len(a)), strings.Compare(a, b))
	})
	return names
}

// hasName reports whether text starts with the known name, ignoring case, and
// returns the text as it was printed.
func hasName(text, name string) (string, bool) {
	if len(text) < len(name) || !strings.EqualFold(text[:len(name)], name) {
		return "", false
	}
	return text[:len(name)], true
}

// pairLine is one way to cut a pair line: the two names as printed, and the
// fields of the text after them.
type pairLine struct {
	a, b   string
	fields []string
}

// splitPairs returns every way to read text as "<a> vs <b>" followed by the
// numbers, where both names are schedulers of this log. A name may contain
// " vs " itself, so each occurrence of it is tried as the separator. A pair
// line is only accepted when exactly one reading is left.
func (p *parser) splitPairs(text string) []pairLine {
	var out []pairLine
	for from := 0; ; {
		i := strings.Index(text[from:], " vs ")
		if i < 0 {
			return out
		}
		at := from + i
		from = at + 1
		a, rest := text[:at], text[at+len(" vs "):]
		if !slices.ContainsFunc(p.known, func(n string) bool { return strings.EqualFold(a, n) }) {
			continue
		}
		for _, name := range p.known {
			b, ok := hasName(rest, name)
			if !ok {
				continue
			}
			if m := pairTailRE.FindStringSubmatch(rest[len(b):]); m != nil {
				out = append(out, pairLine{a: a, b: b, fields: m[1:]})
			}
		}
	}
}

// onlyLine is one way to cut an only line: the name as printed and the fields
// of the text after it.
type onlyLine struct {
	name   string
	fields []string
}

// splitOnly returns every way to read the text after "only " as a scheduler of
// this log followed by the numbers.
func (p *parser) splitOnly(rest string) []onlyLine {
	var out []onlyLine
	for _, name := range p.known {
		n, ok := hasName(rest, name)
		if !ok {
			continue
		}
		if m := onlyTailRE.FindStringSubmatch(rest[len(n):]); m != nil {
			out = append(out, onlyLine{name: n, fields: m[1:]})
		}
	}
	return out
}

func (p *parser) addPair(g *Group, l pairLine) error {
	m := l.fields
	var n numReader
	pair := Pair{A: l.a, B: l.b, NA: n.count(m[0]), NB: n.count(m[1]), Magnitude: m[7]}
	pair.MeanA = n.float(m[2])
	pair.MinA, pair.MaxA = n.span(m[3])
	pair.MeanB = n.float(m[4])
	pair.MinB, pair.MaxB = n.span(m[5])
	pair.Cliff = n.float(m[6])
	pair.P = n.float(m[8])
	if m[9] == ">20" {
		pair.PowerAbove = true
	} else {
		pair.Power = n.count(m[9])
	}
	if n.err != nil {
		return p.fail("pair line has a bad number")
	}
	if len(g.Standings) > 0 || len(g.Only) > 0 {
		return p.fail("pair line after the MCB or only line of its cell")
	}
	g.Pairs = append(g.Pairs, pair)
	return nil
}

func (p *parser) addOnly(g *Group, l onlyLine) error {
	m := l.fields
	var n numReader
	o := Only{Scheduler: l.name, N: n.count(m[0]), Mean: n.float(m[1])}
	o.Min, o.Max = n.span(m[2])
	if n.err != nil {
		return p.fail("only line has a bad number")
	}
	if len(g.Pairs) > 0 || len(g.Only) > 0 || len(g.Standings) > 0 {
		return p.fail("only line in a cell that already has lines")
	}
	g.Only = append(g.Only, o)
	return nil
}

// addStandings reads the MCB line. Its direction has to agree with the one the
// family name states, since a mismatch would invert every verdict downstream.
func (p *parser) addStandings(g *Group, m []string) error {
	dir := HigherIsBetter
	if m[1] == "lower" {
		dir = LowerIsBetter
	}
	if f := p.family(); f.Direction == Unknown {
		f.Direction = dir
	} else if f.Direction != dir {
		return p.fail("MCB says %s=better but the family name says otherwise", m[1])
	}
	if len(g.Standings) > 0 || len(g.Only) > 0 {
		return p.fail("second MCB line in a cell")
	}
	for entries := m[2]; entries != ""; {
		st, size, ok := p.standing(entries)
		if !ok {
			return p.fail("unreadable MCB entry %q, expected <scheduler>=best, tied or not-best", entries)
		}
		g.Standings = append(g.Standings, st)
		// Entries are separated by one space, so a second space leaves an
		// entry that starts with a space, which no name does.
		entries = strings.TrimPrefix(entries[size:], " ")
	}
	return nil
}

var verdicts = map[string]Verdict{"best": Best, "tied": Tied, "not-best": NotBest}

// standing reads one "<name>=<verdict>" entry from the start of text and
// returns it with its length. The name must be a scheduler of this log and the
// verdict must be followed by a space or the end, so that a name which holds
// an "=" is not cut short. Longer names are tried first.
func (p *parser) standing(text string) (Standing, int, bool) {
	for _, name := range p.known {
		printed, ok := hasName(text, name)
		if !ok || !strings.HasPrefix(text[len(printed):], "=") {
			continue
		}
		after := text[len(printed)+1:]
		for word, verdict := range verdicts {
			if rest, ok := strings.CutPrefix(after, word); ok && (rest == "" || rest[0] == ' ') {
				return Standing{Scheduler: printed, Verdict: verdict}, len(text) - len(rest), true
			}
		}
	}
	return Standing{}, 0, false
}
