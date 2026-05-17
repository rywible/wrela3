package sem

import (
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

const overlappingArenaSource = `
module examples.bad_arena_overlap
use { BootPanic } from platform.hardware.panic
use { ArenaIdentity, ArenaPolicy, PhysicalRegionAuthority } from platform.hardware.memory

class GoodRoot {
    fn build(self) {
        let region = PhysicalRegionAuthority(base = 0x200000, length = 0x10000, align = 4096, provenance = 1, panic = BootPanic())
        let root = region.create_arena(identity = ArenaIdentity(label = "root"), policy = ArenaPolicy(evict_cache_by_default = true))
        let a = root.child_at(identity = ArenaIdentity(label = "a"), offset = 0, length = 8192, align = 4096)
        let b = root.child_at(identity = ArenaIdentity(label = "b"), offset = 4096, length = 8192, align = 4096)
    }
}`

func TestArenaGraphRejectsStaticOverlap(t *testing.T) {
	_, ds := checkTrustedPlatformSourceForTest(t, "platform.test.bad_arena_overlap", overlappingArenaSource)
	if !hasCode(ds, diag.SEM0058) {
		t.Fatalf("expected SEM0058, got %#v", ds)
	}
}

func TestArenaGraphRejectsDuplicateIdentity(t *testing.T) {
	src := strings.ReplaceAll(overlappingArenaSource, `label = "b"`, `label = "a"`)
	_, ds := checkTrustedPlatformSourceForTest(t, "platform.test.duplicate_arena", src)
	if !hasCode(ds, diag.SEM0057) {
		t.Fatalf("expected SEM0057, got %#v", ds)
	}
}
