package sem

import "github.com/ryanwible/wrela3/compiler/report"

func BuildImageReport(checked *CheckedProgram) report.ImageReport {
	r := report.NewImageReport(imageNameForReport(checked))
	if checked == nil {
		return r
	}

	for _, root := range checked.ImageGraph.MemoryRoots {
		r.Memory.RootRegions = append(r.Memory.RootRegions, report.MemoryRootReport{
			Label: root.Label,
			Base:  root.Base,
			Bytes: root.Bytes,
		})
		r.Memory.TotalBytes += root.Bytes
		r.AuthorityAudit.MemoryRoots = append(r.AuthorityAudit.MemoryRoots, report.AuthorityRecord{
			Kind:       "memory_root",
			Label:      root.Label,
			Provenance: "firmware",
		})
	}

	for _, arena := range checked.ImageGraph.Arenas {
		r.Memory.Arenas = append(r.Memory.Arenas, report.ArenaReport{
			Label:  arena.Label,
			Parent: arena.Parent,
			Base:   arena.Base,
			Bytes:  arena.Bytes,
			Owner:  arena.Owner,
		})
		r.AuthorityAudit.Arenas = append(r.AuthorityAudit.Arenas, report.AuthorityRecord{
			Kind:  "arena",
			Label: arena.Label,
			Owner: arena.Owner,
		})
	}

	return r
}

func imageNameForReport(checked *CheckedProgram) string {
	if checked == nil || checked.Index == nil || len(checked.Index.Images) == 0 {
		return "image"
	}
	if checked.Index.Images[0] == nil {
		return "image"
	}
	if checked.Index.Images[0].Name != "" {
		return checked.Index.Images[0].Name
	}
	return "image"
}
