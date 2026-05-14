package source

import "sort"

type FileID int

type Span struct {
	Start int
	End   int
}

type File struct {
	ID      FileID
	Path    string
	Source  string
	Module  string
	lineMap []int
}

func NewFile(id FileID, filePath, source string) *File {
	lineMap := []int{0}
	for i := 0; i < len(source); i++ {
		if source[i] == '\n' {
			lineMap = append(lineMap, i+1)
		}
	}
	return &File{
		ID:      id,
		Path:    filePath,
		Source:  source,
		lineMap: lineMap,
	}
}

func (f *File) LineColumn(offset int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(f.Source) {
		offset = len(f.Source)
	}

	line := sort.Search(len(f.lineMap), func(i int) bool {
		return f.lineMap[i] > offset
	})
	line--
	if line < 0 {
		line = 0
	}
	column := offset - f.lineMap[line] + 1
	return line + 1, column
}
