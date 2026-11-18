// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Galih Tama <galpt@v.recipes>

package render

import "testing"

func TestParseFont(t *testing.T) {
	tests := []struct {
		spec string
		want faceKey
	}{
		{"12px sans-serif", faceKey{false, 12}},
		{`bold 16px "Liberation Serif"`, faceKey{true, 16}},
		{"normal bolder 14px serif", faceKey{true, 14}},
		{"italic 600 13px Liberation Serif", faceKey{true, 13}},
		{"italic 500 13px Liberation Serif", faceKey{false, 13}},
		{"lighter 11.5px/1.4 monospace", faceKey{false, 11.5}},
		{`12px "Arial Bold"`, faceKey{false, 12}},
		{"700 10px x", faceKey{true, 10}},
		{"", faceKey{false, defaultFontSize}},
		{"bold serif", faceKey{true, defaultFontSize}},
	}
	for _, tt := range tests {
		if got := parseFont(tt.spec); got != tt.want {
			t.Errorf("parseFont(%q) = %+v, want %+v", tt.spec, got, tt.want)
		}
	}
}

func TestMeasureWidth(t *testing.T) {
	m, err := newMeasurer()
	if err != nil {
		t.Fatal(err)
	}
	regular := m.width("Frame time p99", "12px sans-serif")
	bold := m.width("Frame time p99", "bold 12px sans-serif")
	large := m.width("Frame time p99", "24px sans-serif")
	if regular <= 0 || bold <= regular {
		t.Errorf("regular = %v, bold = %v, want 0 < regular < bold", regular, bold)
	}
	if d := large - 2*regular; d < -0.5 || d > 0.5 {
		t.Errorf("24px width %v is not twice the 12px width %v", large, regular)
	}
	if m.width("", "12px serif") != 0 {
		t.Error("empty text has width")
	}
}
