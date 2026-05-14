package asm

import "testing"

func TestLookupRegister(t *testing.T) {
	cases := []struct {
		name    string
		width   int
		low3    int
		high    bool
		control bool
	}{
		{name: "rax", width: 64, low3: 0, high: false, control: false},
		{name: "al", width: 8, low3: 0, high: false, control: false},
		{name: "r8", width: 64, low3: 0, high: true, control: false},
		{name: "r15b", width: 8, low3: 7, high: true, control: false},
		{name: "cr3", width: 64, low3: 3, high: false, control: true},
	}

	for _, c := range cases {
		r, ok := Lookup(c.name)
		if !ok {
			t.Fatalf("Lookup(%q) not found", c.name)
		}
		if r.Width != c.width {
			t.Fatalf("%s width = %d, want %d", c.name, r.Width, c.width)
		}
		if r.Low3 != c.low3 {
			t.Fatalf("%s low3 = %d, want %d", c.name, r.Low3, c.low3)
		}
		if r.High != c.high {
			t.Fatalf("%s high = %t, want %t", c.name, r.High, c.high)
		}
		if r.Control != c.control {
			t.Fatalf("%s control = %t, want %t", c.name, r.Control, c.control)
		}
	}
}
