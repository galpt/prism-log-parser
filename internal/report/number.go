// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package report

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

var errBadNumber = errors.New("bad number")

// parseNumber reads a decimal float the way Python and C print one. Go's
// ParseFloat is wider than that: it takes hex floats and underscores, which
// would let a corrupted cell through, and it refuses a signed NaN that glibc
// prints as "-nan".
func parseNumber(s string) (float64, error) {
	if strings.ContainsAny(s, "xXpP_") {
		return 0, errBadNumber
	}
	unsigned := s
	if s != "" && (s[0] == '+' || s[0] == '-') {
		unsigned = s[1:]
	}
	if strings.EqualFold(unsigned, "nan") {
		return math.NaN(), nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, errBadNumber
	}
	return v, nil
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// numReader converts several fields of one line and remembers the first
// failure, so a line parser can read all its numbers and check once.
type numReader struct {
	err error
}

func (n *numReader) float(s string) float64 {
	v, err := parseNumber(s)
	if err != nil && n.err == nil {
		n.err = err
	}
	return v
}

func (n *numReader) count(s string) int {
	v, err := strconv.Atoi(s)
	if err != nil && n.err == nil {
		n.err = errBadNumber
	}
	return v
}

// span reads the "lo..hi" text of a min..max range.
func (n *numReader) span(s string) (lo, hi float64) {
	l, h, ok := strings.Cut(s, "..")
	if !ok && n.err == nil {
		n.err = errBadNumber
	}
	return n.float(l), n.float(h)
}
