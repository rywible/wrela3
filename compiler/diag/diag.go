package diag

import "sort"

type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
)

type Diagnostic struct {
	Phase    string
	Code     string
	Severity Severity
	FilePath string
	Start    int
	End      int
	Message  string
	Sequence int
}

func Sort(ds []Diagnostic) {
	sort.SliceStable(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		if a.Phase != b.Phase {
			return a.Phase < b.Phase
		}
		if a.FilePath != b.FilePath {
			return a.FilePath < b.FilePath
		}
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		if a.End != b.End {
			return a.End < b.End
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Sequence < b.Sequence
	})
}
