package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ryanwible/wrela3/compiler"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 || args[0] != "build" {
		fmt.Fprintln(os.Stderr, "usage: wrela build --mode dev <root.wrela> -o <out.efi>")
		return 2
	}

	modeRaw, root, output, repoRoot, ok := parseBuildArgs(args[1:])
	if !ok || root == "" {
		fmt.Fprintln(os.Stderr, "usage: wrela build --mode dev <root.wrela> -o <out.efi>")
		return 2
	}

	mode, err := compiler.ParseMode(modeRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	result, err := compiler.Build(compiler.BuildOptions{
		Mode:       mode,
		RootPath:   root,
		OutputPath: output,
		RepoRoot:   repoRoot,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if ce, ok := err.(compiler.CodeError); ok && strings.HasPrefix(ce.Code, "CLI") {
			return 2
		}
		if ce, ok := err.(compiler.CodeError); ok && strings.HasPrefix(ce.Code, "INT") {
			return 3
		}
		return 1
	}
	fmt.Fprintln(os.Stdout, result.OutputPath)
	return 0
}

func parseBuildArgs(args []string) (mode, root, output, repoRoot string, ok bool) {
	repoRoot = "."
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--mode":
			i++
			if i >= len(args) {
				return "", "", "", "", false
			}
			mode = args[i]
		case strings.HasPrefix(arg, "--mode="):
			mode = strings.TrimPrefix(arg, "--mode=")
		case arg == "-o":
			i++
			if i >= len(args) {
				return "", "", "", "", false
			}
			output = args[i]
		case arg == "--repo-root":
			i++
			if i >= len(args) {
				return "", "", "", "", false
			}
			repoRoot = args[i]
		case strings.HasPrefix(arg, "--repo-root="):
			repoRoot = strings.TrimPrefix(arg, "--repo-root=")
		case strings.HasPrefix(arg, "-"):
			return "", "", "", "", false
		default:
			if root != "" {
				return "", "", "", "", false
			}
			root = arg
		}
	}
	return mode, root, output, repoRoot, true
}
