package diag

import (
	"fmt"
	"strings"
)

func Render(ds []Diagnostic) string {
	Sort(ds)
	var b strings.Builder
	for _, d := range ds {
		fmt.Fprintf(&b, "%s:%d-%d: %s %s: %s\n", d.FilePath, d.Start, d.End, d.Severity, d.Code, d.Message)
	}
	return b.String()
}
