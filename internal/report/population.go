// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package report

import (
	"regexp"
	"strings"
	"unicode"
)

// The line shapes below come from the cell_text printer in montauk's
// prom_population.cpp. Only the forms that prism-frame can trigger are
// accepted, because it passes neither --relative-to nor --full. Names are
// printed lowercase and never contain a space.
var (
	pairRE = regexp.MustCompile(`^(\S+) vs (\S+) \[N=(\d+)/(\d+)\]: ` +
		`mean (\S+) \(([^)]*)\) vs (\S+) \(([^)]*)\) \| ` +
		`cliff (\S+) \((\S+)\) \| perm p=(\S+) \| power (\d+|>20)$`)
	onlyRE = regexp.MustCompile(`^only (\S+) \[N=(\d+)\]: mean (\S+) \(([^)]*)\) -- no comparison$`)
	mcbRE  = regexp.MustCompile(`^MCB \((higher|lower)=better\):((?: \S+=\S+)+)$`)
)

const onlyNoneLine = "only (none) -- no comparison"

// populationHeader expects the POPULATION line that prism-frame writes before
// pasting montauk's output.
func populationHeader(p *parser, line string) (stateFn, error) {
	if line != populationLine {
		return nil, p.fail("expected %q", populationLine)
	}
	p.inPopulation = true
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
	if m := pairRE.FindStringSubmatch(text); m != nil {
		return p.addPair(g, m)
	}
	if m := onlyRE.FindStringSubmatch(text); m != nil {
		return p.addOnly(g, m)
	}
	if text == onlyNoneLine {
		g.Only = append(g.Only, Only{})
		return nil
	}
	if m := mcbRE.FindStringSubmatch(text); m != nil {
		return p.addStandings(g, m)
	}
	return p.fail("unknown line in the POPULATION section")
}

func (p *parser) addPair(g *Group, m []string) error {
	var n numReader
	pair := Pair{A: m[1], B: m[2], NA: n.count(m[3]), NB: n.count(m[4]), Magnitude: m[10]}
	pair.MeanA = n.float(m[5])
	pair.MinA, pair.MaxA = n.span(m[6])
	pair.MeanB = n.float(m[7])
	pair.MinB, pair.MaxB = n.span(m[8])
	pair.Cliff = n.float(m[9])
	pair.P = n.float(m[11])
	if m[12] == ">20" {
		pair.PowerAbove = true
	} else {
		pair.Power = n.count(m[12])
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

func (p *parser) addOnly(g *Group, m []string) error {
	var n numReader
	o := Only{Scheduler: m[1], N: n.count(m[2]), Mean: n.float(m[3])}
	o.Min, o.Max = n.span(m[4])
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
	for _, field := range strings.Fields(m[2]) {
		cut := strings.LastIndex(field, "=")
		verdict, ok := verdicts[field[cut+1:]]
		if !ok {
			return p.fail("unknown MCB verdict %q", field[cut+1:])
		}
		g.Standings = append(g.Standings, Standing{Scheduler: field[:cut], Verdict: verdict})
	}
	return nil
}

var verdicts = map[string]Verdict{"best": Best, "tied": Tied, "not-best": NotBest}
