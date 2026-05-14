package compiler

type BuildOptions struct {
	Mode       Mode
	RootPath   string
	OutputPath string
	RepoRoot   string
}

type BuildResult struct {
	OutputPath string
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
	return BuildResult{}, NewCodeError("INT0001", "build pipeline is not wired yet")
}
