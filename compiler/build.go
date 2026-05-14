package compiler

import (
	"os"
	"path/filepath"

	"github.com/ryanwible/wrela3/compiler/codegen"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/ir"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/pecoff"
	"github.com/ryanwible/wrela3/compiler/sem"
	"github.com/ryanwible/wrela3/compiler/source"
)

type BuildOptions struct {
	Mode       Mode
	RootPath   string
	OutputPath string
	RepoRoot   string
}

type BuildResult struct {
	OutputPath string
}

type DiagnosticError struct {
	Diagnostics []diag.Diagnostic
}

func (e DiagnosticError) Error() string {
	return diag.Render(e.Diagnostics)
}

func Build(opts BuildOptions) (BuildResult, error) {
	if opts.Mode == ModeRelease {
		return BuildResult{}, NewCodeError("CLI0002", "release mode is not implemented in v0")
	}
	if opts.RootPath == "" {
		return BuildResult{}, NewCodeError("CLI0003", "root source path is required")
	}
	if opts.OutputPath == "" {
		return BuildResult{}, NewCodeError("CLI0004", "output path is required")
	}
	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		repoRoot = "."
	}
	repoRoot = resolveRepoRoot(repoRoot)
	rootPath := opts.RootPath
	if !filepath.IsAbs(rootPath) {
		rootPath = filepath.Join(repoRoot, rootPath)
	}
	outputPath := opts.OutputPath
	if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(repoRoot, outputPath)
	}
	outputPath = filepath.Clean(outputPath)
	graph, err := source.LoadGraph(source.Options{
		RootPath: rootPath,
		ImportRoots: []string{
			repoRoot,
			filepath.Join(repoRoot, "wrela"),
		},
	})
	if err != nil {
		return BuildResult{}, err
	}
	modules, ds := parse.ParseGraph(*graph)
	if len(ds) != 0 {
		return BuildResult{}, DiagnosticError{Diagnostics: ds}
	}
	index, ds := sem.BuildIndex(modules)
	if len(ds) != 0 {
		return BuildResult{}, DiagnosticError{Diagnostics: ds}
	}
	checked, ds := sem.Check(index, modules)
	if len(ds) != 0 {
		return BuildResult{}, DiagnosticError{Diagnostics: ds}
	}
	program, ds := ir.Lower(checked)
	if len(ds) != 0 {
		return BuildResult{}, DiagnosticError{Diagnostics: ds}
	}
	image, ds := codegen.Compile(program)
	if len(ds) != 0 {
		return BuildResult{}, DiagnosticError{Diagnostics: ds}
	}
	bytes, err := pecoff.WriteEFI(image)
	if err != nil {
		return BuildResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return BuildResult{}, err
	}
	if err := os.WriteFile(outputPath, bytes, 0o644); err != nil {
		return BuildResult{}, err
	}
	return BuildResult{OutputPath: outputPath}, nil
}

func resolveRepoRoot(raw string) string {
	if raw == "" {
		raw = "."
	}
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return filepath.Clean(raw)
	}
	if raw != "." {
		return filepath.Clean(filepath.Join(cwd, raw))
	}
	candidate := filepath.Clean(filepath.Join(cwd, raw))
	for {
		if _, err := os.Stat(filepath.Join(candidate, "go.mod")); err == nil {
			return candidate
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return filepath.Clean(filepath.Join(cwd, raw))
		}
		candidate = parent
	}
}
