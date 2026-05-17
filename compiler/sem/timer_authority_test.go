package sem

import (
	"strings"
	"testing"
)

func TestTimerAuthorityPriorityOrder(t *testing.T) {
	sourceText := readRepoFile(t, "wrela/machine/x86_64/timer.wrela")
	local := strings.Index(sourceText, "TimerSource(kind = 1)")
	hpet := strings.Index(sourceText, "TimerSource(kind = 2)")
	fatal := strings.Index(sourceText, "self.panic.fail(code = 0xAC080001)")
	if local < 0 || hpet < 0 || fatal < 0 {
		t.Fatalf("timer source must include local APIC/PIT, HPET fact path, and boot fatal")
	}
	if !(local < hpet && hpet < fatal) {
		t.Fatalf("timer priority order must be local APIC/PIT, HPET, boot fatal")
	}
}
