package source

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ryanwible/wrela3/compiler"
)

type Options struct {
	RootPath    string
	ImportRoots []string
}

type Graph struct {
	Files []*File
}

var (
	modulePattern = regexp.MustCompile(`^module\s+([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\s*$`)
	importPattern = regexp.MustCompile(`^use\s+\{[^}]*\}\s+from\s+([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\s*$`)
)

func ExtractHeader(source string) (module string, imports []string, err error) {
	moduleFound := false
	imports = []string{}
	for _, raw := range strings.Split(source, "\n") {
		line := strings.TrimSpace(stripInlineComment(raw))
		if line == "" {
			continue
		}
		if !moduleFound {
			match := modulePattern.FindStringSubmatch(line)
			if len(match) != 2 {
				return "", nil, compiler.NewCodeError("SRC0002", fmt.Sprintf("invalid module header %q", line))
			}
			module = match[1]
			moduleFound = true
			continue
		}
		if strings.HasPrefix(line, "use ") {
			match := importPattern.FindStringSubmatch(line)
			if len(match) != 2 {
				return "", nil, compiler.NewCodeError("SRC0002", fmt.Sprintf("invalid import line %q", line))
			}
			imports = append(imports, match[1])
			continue
		}
		break
	}
	if !moduleFound {
		return "", nil, compiler.NewCodeError("SRC0002", "missing module declaration")
	}
	return module, imports, nil
}

func LoadGraph(opts Options) (*Graph, error) {
	graph := &Graph{}
	if opts.RootPath == "" {
		return nil, compiler.NewCodeError("SRC0002", "missing root path")
	}
	rootPath := filepath.Clean(opts.RootPath)
	rootSource, err := os.ReadFile(rootPath)
	if err != nil {
		return nil, compiler.NewCodeError("SRC0001", fmt.Sprintf("could not read %q: %v", rootPath, err))
	}

	rootModule, _, err := ExtractHeader(string(rootSource))
	if err != nil {
		return nil, err
	}

	importedModules := &moduleLoadState{
		options:    opts,
		graph:      graph,
		loaded:     map[string]*File{},
		inProgress: map[string]bool{},
		nextFileID: 1,
	}
	return importedModules.loadModule(rootModule, rootPath)
}

type moduleLoadState struct {
	options    Options
	graph      *Graph
	loaded     map[string]*File
	inProgress map[string]bool
	nextFileID FileID
}

func (s *moduleLoadState) loadModule(moduleName, path string) (*Graph, error) {
	if s.inProgress[moduleName] {
		return nil, compiler.NewCodeError("SRC0004", fmt.Sprintf("import cycle at %q", moduleName))
	}
	if existing, ok := s.loaded[moduleName]; ok {
		return nil, compiler.NewCodeError("SRC0005", fmt.Sprintf("duplicate module %q", existing.Module))
	}
	s.inProgress[moduleName] = true

	data, err := os.ReadFile(path)
	if err != nil {
		delete(s.inProgress, moduleName)
		return nil, compiler.NewCodeError("SRC0001", fmt.Sprintf("could not read %q: %v", path, err))
	}

	actualModule, imports, err := ExtractHeader(string(data))
	if err != nil {
		delete(s.inProgress, moduleName)
		return nil, err
	}
	if actualModule != moduleName {
		delete(s.inProgress, moduleName)
		return nil, compiler.NewCodeError("SRC0002", fmt.Sprintf("module mismatch %q does not match requested %q", actualModule, moduleName))
	}

	f := NewFile(s.nextFileID, path, string(data))
	f.Module = actualModule
	s.nextFileID++
	s.graph.Files = append(s.graph.Files, f)
	s.loaded[actualModule] = f

	for _, imported := range imports {
		nextPath, found, err := s.resolveModulePath(imported)
		if err != nil {
			delete(s.inProgress, moduleName)
			return nil, err
		}
		if !found {
			delete(s.inProgress, moduleName)
			return nil, compiler.NewCodeError("SRC0003", fmt.Sprintf("module not found: %q", imported))
		}
		if _, err := s.loadModule(imported, nextPath); err != nil {
			delete(s.inProgress, moduleName)
			return nil, err
		}
	}
	delete(s.inProgress, moduleName)
	return s.graph, nil
}

func (s *moduleLoadState) resolveModulePath(module string) (string, bool, error) {
	rel := filepath.FromSlash(strings.ReplaceAll(module, ".", "/")) + ".wrela"
	for _, root := range s.options.ImportRoots {
		candidate := filepath.Join(root, rel)
		_, err := os.ReadFile(candidate)
		if err == nil {
			return candidate, true, nil
		}
		if !os.IsNotExist(err) {
			return "", false, compiler.NewCodeError("SRC0001", fmt.Sprintf("could not read %q: %v", candidate, err))
		}
	}
	return "", false, nil
}

func stripInlineComment(raw string) string {
	if idx := strings.Index(raw, "//"); idx >= 0 {
		return raw[:idx]
	}
	return raw
}
