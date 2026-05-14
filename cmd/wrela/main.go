package main

import (
	"flag"
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

	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	modeRaw := fs.String("mode", "", "compile mode: dev or release")
	output := fs.String("o", "", "output .efi path")
	repoRoot := fs.String("repo-root", ".", "repository root containing wrela/")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: wrela build --mode dev <root.wrela> -o <out.efi>")
		return 2
	}

	mode, err := compiler.ParseMode(*modeRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	_, err = compiler.Build(compiler.BuildOptions{
		Mode:       mode,
		RootPath:   fs.Arg(0),
		OutputPath: *output,
		RepoRoot:   *repoRoot,
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
	return 0
}
