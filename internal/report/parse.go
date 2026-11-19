// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package report

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// maxLine bounds one log line. The widest real line is a table row, a few
// hundred bytes, so this only stops a corrupt file from exhausting memory.
const maxLine = 1 << 20

const (
	titleLine      = "PANDEMONIUM PRISM-FRAME"
	footerLine     = "vs EEVDF rows are mean deltas; significance is montauk's, below."
	populationLine = "POPULATION (montauk, per-iteration .prom files)"
)

// stateFn handles one line and returns the handler for the next one. Each
// section of the log has its own, so a line is only ever checked against the
// grammar of the section it appears in.
type stateFn func(p *parser, line string) (stateFn, error)

type parser struct {
	name string
	// line is the 1-based number of the line being handled and text is that
	// line without trailing whitespace.
	line int
	text string
	rep  *Report

	// stage counts the preamble fields read so far.
	stage int
	// seen holds the labels of the current scenario, to refuse a repeated row.
	seen map[string]bool
	// head is the header row of the current scenario while its scheduler names
	// are still unresolved.
	head *pendingHeader
	// known holds the lowercase scheduler names of the tables, longest first,
	// for reading the POPULATION lines.
	known []string
	// footerSeen and inPopulation record how far the file got, for the checks
	// that run at the end of input.
	footerSeen   bool
	inPopulation bool
	// famOpen and grpOpen tell whether a METRIC block and a "[...]" cell are
	// open, so that a population line is only accepted inside one.
	famOpen bool
	grpOpen bool
	// freeText tells whether the paragraph being read is montauk's stderr text,
	// which is copied line by line rather than parsed.
	freeText bool
}

// Parse reads one log. The name only labels errors, which read
// "<name>:<line>: <problem>: <quoted line>".
func Parse(name string, r io.Reader) (*Report, error) {
	p := &parser{name: name, rep: &Report{Source: name}}
	sc := bufio.NewScanner(r)
	sc.Buffer(nil, maxLine)
	state := stateFn(preamble)
	for sc.Scan() {
		p.line++
		// A log written on Windows or edited by hand can carry CR and trailing
		// blanks, none of which the writer prints.
		p.text = strings.TrimRight(sc.Text(), " \t\r")
		var err error
		if state, err = state(p, p.text); err != nil {
			return nil, err
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s:%d: %w", name, p.line+1, err)
	}
	if err := p.finish(); err != nil {
		return nil, err
	}
	return p.rep, nil
}

// ParseFile parses the log at path.
func ParseFile(path string) (*Report, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(path, f)
}

// fail builds an error for the current line.
func (p *parser) fail(format string, args ...any) error {
	return p.failAt(p.line, p.text, format, args...)
}

// failAt builds an error for an earlier line, such as a header that could only
// be checked once the rows below it were read.
func (p *parser) failAt(line int, text, format string, args ...any) error {
	return fmt.Errorf("%s:%d: %s: %q", p.name, line, fmt.Sprintf(format, args...), text)
}

// finish checks what only the end of the input can tell. The reported line is
// the one that would have come next, which is 1 for an empty file.
func (p *parser) finish() error {
	problem := ""
	switch {
	case p.line == 0:
		problem = "empty file"
	case p.stage < len(preambleFields):
		problem = "unexpected end of file in the preamble, missing " + preambleFields[p.stage].tag
	case len(p.rep.Scenarios) == 0:
		problem = "unexpected end of file, no SCENARIO block"
	case !p.footerSeen:
		problem = "unexpected end of file, missing the footer line"
	case p.inPopulation && len(p.rep.Population) == 0 && len(p.rep.PopulationText) == 0:
		problem = "unexpected end of file, the POPULATION section is empty"
	}
	if problem != "" {
		return fmt.Errorf("%s:%d: %s", p.name, p.line+1, problem)
	}
	return nil
}

var preambleFields = []struct {
	tag string
	set func(m *Meta, value string) error
}{
	{"VERSION:", func(m *Meta, v string) error { m.Version = v; return nil }},
	{"COMMIT:", setCommit},
	{"TIMESTAMP:", setTimestamp},
	{"CPUS:", func(m *Meta, v string) error { return setCount(&m.CPUs, v) }},
	{"ITERATIONS:", func(m *Meta, v string) error { return setCount(&m.Iterations, v) }},
	{"CALIBRATION:", setCalibration},
}

// preamble handles the title, the fixed sequence of fields and the blank line
// that ends them.
func preamble(p *parser, line string) (stateFn, error) {
	if p.line == 1 {
		if line != titleLine {
			return nil, p.fail("not a PRISM-FRAME log, expected %q on the first line", titleLine)
		}
		return preamble, nil
	}
	if p.stage == len(preambleFields) {
		if line != "" {
			return nil, p.fail("expected a blank line after the preamble")
		}
		return scenarioStart, nil
	}
	field := preambleFields[p.stage]
	value, ok := strings.CutPrefix(line, field.tag+" ")
	if !ok || strings.TrimSpace(value) == "" {
		return nil, p.fail("expected %s with a value", field.tag)
	}
	if err := field.set(&p.rep.Meta, strings.TrimSpace(value)); err != nil {
		return nil, p.fail("bad %s: %v", strings.TrimSuffix(field.tag, ":"), err)
	}
	p.stage++
	return preamble, nil
}

func setCommit(m *Meta, v string) error {
	// The leading space lets a log whose commit is empty, which prints just
	// "(dirty)", reach the emptiness check below instead of passing as a hash.
	if hash, ok := strings.CutSuffix(" "+v, " (dirty)"); ok {
		m.Dirty = true
		v = strings.TrimSpace(hash)
	}
	if v == "" {
		return fmt.Errorf("commit is empty")
	}
	m.Commit = v
	return nil
}

// timestampRE is the strftime("%Y%m%d-%H%M%S") stamp the writer prints. Callers
// rely on it containing no spaces when they build labels from it.
var timestampRE = regexp.MustCompile(`^\d{8}-\d{6}$`)

func setTimestamp(m *Meta, v string) error {
	if !timestampRE.MatchString(v) {
		return fmt.Errorf("timestamp is not YYYYMMDD-HHMMSS")
	}
	m.Timestamp = v
	return nil
}

func setCount(dst *int, v string) error {
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return fmt.Errorf("count is not a positive integer")
	}
	*dst = n
	return nil
}

var calibrationRE = regexp.MustCompile(`^(\S+) iterations/us(?:  \(end-of-run drift (\S+)%\))?$`)

func setCalibration(m *Meta, v string) error {
	match := calibrationRE.FindStringSubmatch(v)
	if match == nil {
		return fmt.Errorf("expected \"<n> iterations/us\" with an optional drift")
	}
	var n numReader
	m.Calibration = n.float(match[1])
	if match[2] != "" {
		m.Drift = n.float(match[2])
		m.HasDrift = true
	}
	if n.err != nil || !finite(m.Calibration) || !finite(m.Drift) {
		return fmt.Errorf("calibration is not a finite number")
	}
	return nil
}

// footer handles the lines after the last table. The AUDIO and SKIPPED lines
// are optional and a blank line moves on to the population section. A scheduler
// name can hold spaces, so a SKIPPED line is cut at its first " -- ".
func footer(p *parser, line string) (stateFn, error) {
	if line == "" {
		return populationHeader, nil
	}
	if note, ok := strings.CutPrefix(line, "AUDIO: "); ok && note != "" {
		p.rep.Notes = append(p.rep.Notes, note)
		return footer, nil
	}
	if rest, ok := strings.CutPrefix(line, "SKIPPED: "); ok {
		sched, reason, found := strings.Cut(rest, " -- ")
		sched, reason = strings.TrimSpace(sched), strings.TrimSpace(reason)
		if !found || sched == "" || reason == "" {
			return nil, p.fail("expected SKIPPED: <scheduler> -- <reason>")
		}
		p.rep.Skipped = append(p.rep.Skipped, Skip{Scheduler: sched, Reason: reason})
		return footer, nil
	}
	return nil, p.fail("unknown line in the footer")
}
