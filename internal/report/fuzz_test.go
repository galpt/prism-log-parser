// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package report

import (
	"math"
	"strings"
	"testing"
)

// FuzzParse checks that no input makes Parse panic or hang, and that a log it
// accepts has the shape the rest of the program relies on.
func FuzzParse(f *testing.F) {
	full := fullLog()
	f.Add(full)
	f.Add(strings.ReplaceAll(full, "\n", "\r\n"))
	f.Add(synthLog(spacedNames, []string{"alpha (bpf) -- crashed"}, spacedPopulation))
	f.Add(synthLog([]string{"EEVDF", "PANDEMONIUM (BPF)", "PANDEMONIUM (ADAPTIVE)", "scx_panda", "scx_flow"}, nil, ""))
	f.Add("")
	f.Add("PANDEMONIUM PRISM-FRAME\n")
	f.Add(full[:strings.Index(full, "SCENARIO BETA")])
	f.Add(full[:strings.Index(full, "METRIC pandemonium_frame_lower")])
	f.Add(strings.ReplaceAll(full, "±", "+-"))
	f.Add(strings.ReplaceAll(full, "          ", "\t"))
	f.Fuzz(func(t *testing.T, text string) {
		r, err := Parse("fuzz.log", strings.NewReader(text))
		if err != nil {
			if r != nil {
				t.Fatal("report returned together with an error")
			}
			return
		}
		if len(r.Scenarios) == 0 {
			t.Fatal("accepted a log without scenarios")
		}
		for _, sc := range r.Scenarios {
			if len(sc.Schedulers) == 0 {
				t.Fatalf("scenario %s has no schedulers", sc.Name)
			}
			for _, name := range sc.Schedulers {
				if name == "" || name != strings.Trim(name, " ") {
					t.Fatalf("scenario %s has the scheduler name %q", sc.Name, name)
				}
			}
			for _, row := range sc.Rows {
				if len(row.Cells) != len(sc.Schedulers) {
					t.Fatalf("row %q has %d cells for %d schedulers", row.Metric.Label, len(row.Cells), len(sc.Schedulers))
				}
				for _, c := range row.Cells {
					if c.Valid && (!finite(c.Mean) || !finite(c.SD)) {
						t.Fatalf("valid cell %+v is not finite", c)
					}
				}
			}
			for _, d := range sc.Deltas {
				if len(d.Cells) != len(sc.Schedulers) {
					t.Fatalf("delta %q has %d cells for %d schedulers", d.Metric.Label, len(d.Cells), len(sc.Schedulers))
				}
				for _, c := range d.Cells {
					if c.Valid && math.IsNaN(c.Percent) {
						t.Fatalf("valid delta %+v is NaN", c)
					}
				}
			}
		}
	})
}
