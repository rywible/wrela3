package ir

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/sem"
)

func checkedStorageProgramForTest(t *testing.T) *sem.CheckedProgram {
	t.Helper()
	return checkedProgramForTest(t, `
module app
data FileId { value: U64 }
data FileNameKey { value: U64 }
data DirectoryChild { value: U64 }
data OrderedPages<Partition, SortKey, Row> { root: U64 }

event FileCreated id 1001 {
    file_id: FileId
    layout 1 current {
        file_id: U64 = self.file_id.value
    }
}

projection DirectoryChildren id 12 {
    layout 1 current {
        children: OrderedPages<FileId, FileNameKey, DirectoryChild>
    }
}
`)
}
