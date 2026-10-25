// Command relnotes prints the release notes of one version: the body of its
// section in CHANGELOG.md, without the heading. The release workflow passes the
// output to goreleaser as the GitHub release body.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	changelog "github.com/tinchoram/groot-cli"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run prints the section of the version named by args[0], with or without the
// leading v. It exits 1 when CHANGELOG.md has no section for it.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: relnotes <version>, for example relnotes v0.5.3")
		return 2
	}
	version := strings.TrimPrefix(strings.TrimSpace(args[0]), "v")
	section, ok := changelog.Section(version)
	if !ok {
		fmt.Fprintf(stderr, "relnotes: no CHANGELOG.md entry for %s\n", version)
		return 1
	}
	body := ""
	if i := strings.IndexByte(section, '\n'); i >= 0 {
		body = strings.TrimSpace(section[i+1:])
	}
	fmt.Fprintln(stdout, body)
	return 0
}
