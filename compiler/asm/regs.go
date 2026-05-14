package asm

type Reg struct {
	Name    string
	Width   int
	Low3    int
	High    bool
	Control bool
	Segment bool
}

var regs = map[string]Reg{}

func init() {
	add := func(name string, width, low3 int, high, control bool) {
		regs[name] = Reg{
			Name:    name,
			Width:   width,
			Low3:    low3,
			High:    high,
			Control: control,
		}
	}
	addSegment := func(name string, low3 int) {
		regs[name] = Reg{Name: name, Width: 16, Low3: low3, Segment: true}
	}

	add("rax", 64, 0, false, false)
	add("eax", 32, 0, false, false)
	add("ax", 16, 0, false, false)
	add("al", 8, 0, false, false)
	add("ah", 8, 4, false, false)

	add("rcx", 64, 1, false, false)
	add("ecx", 32, 1, false, false)
	add("cx", 16, 1, false, false)
	add("cl", 8, 1, false, false)
	add("ch", 8, 5, false, false)

	add("rdx", 64, 2, false, false)
	add("edx", 32, 2, false, false)
	add("dx", 16, 2, false, false)
	add("dl", 8, 2, false, false)
	add("dh", 8, 6, false, false)

	add("rbx", 64, 3, false, false)
	add("ebx", 32, 3, false, false)
	add("bx", 16, 3, false, false)
	add("bl", 8, 3, false, false)

	add("rsp", 64, 4, false, false)
	add("esp", 32, 4, false, false)
	add("sp", 16, 4, false, false)
	add("spl", 8, 4, true, false)

	add("rbp", 64, 5, false, false)
	add("ebp", 32, 5, false, false)
	add("bp", 16, 5, false, false)
	add("bpl", 8, 5, true, false)

	add("rsi", 64, 6, false, false)
	add("esi", 32, 6, false, false)
	add("si", 16, 6, false, false)
	add("sil", 8, 6, true, false)

	add("rdi", 64, 7, false, false)
	add("edi", 32, 7, false, false)
	add("di", 16, 7, false, false)
	add("dil", 8, 7, true, false)

	for _, w := range []int{64, 32, 16, 8} {
		add("r8"+suffixFromWidth(w), w, 0, true, false)
		add("r9"+suffixFromWidth(w), w, 1, true, false)
		add("r10"+suffixFromWidth(w), w, 2, true, false)
		add("r11"+suffixFromWidth(w), w, 3, true, false)
		add("r12"+suffixFromWidth(w), w, 4, true, false)
		add("r13"+suffixFromWidth(w), w, 5, true, false)
		add("r14"+suffixFromWidth(w), w, 6, true, false)
		add("r15"+suffixFromWidth(w), w, 7, true, false)
	}

	add("cr3", 64, 3, false, true)
	addSegment("es", 0)
	addSegment("ss", 2)
	addSegment("ds", 3)
	addSegment("fs", 4)
	addSegment("gs", 5)
}

func suffixFromWidth(width int) string {
	switch width {
	case 64:
		return ""
	case 32:
		return "d"
	case 16:
		return "w"
	case 8:
		return "b"
	default:
		return ""
	}
}

func Lookup(name string) (Reg, bool) {
	r, ok := regs[name]
	if !ok {
		return Reg{}, false
	}
	return r, true
}

func MustLookup(name string) Reg {
	r, ok := Lookup(name)
	if !ok {
		panic("asm: unknown register " + name)
	}
	return r
}
