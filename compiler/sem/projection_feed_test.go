package sem

import (
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

const projectionWorkerWithoutFeedSource = `
module storage.writer
data CommittedAtomicGroup {
    first_event_id: U64
    last_event_id: U64
}

---

module storage.projections
data ProjectionWriter<P> {
    root_ref: U64
}

---

module sem.projection_feed

use { ProjectionWriter } from storage.projections

projection DirectoryChildren id 12 {
    layout 1 current {
        count: StateCell
    }
}

data StateCell {}

class DirectoryProjectionWorker {
    writer: ProjectionWriter<DirectoryChildren>
}

class Boot {
    fn run(self) {
        let worker = DirectoryProjectionWorker(writer = ProjectionWriter<DirectoryChildren>(root_ref = 0))
    }
}
`

func TestProjectionWorkerRequiresFeed(t *testing.T) {
	modules := parseModulesForTest(t, strings.Split(projectionWorkerWithoutFeedSource, "\n---\n")...)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if !hasCode(ds, diag.SEM0120) {
		t.Fatalf("diagnostics = %#v, want SEM0120", ds)
	}
}
