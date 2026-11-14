package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/farzeni/regia/internal/spawns"
)

// defaultPluginSource is where `regia setup` installs the plugin (the hook)
// from. The repo is public, so https needs no keys on a new machine.
const defaultPluginSource = "https://github.com/farzeni/regia.git"

// setup installs the regia Claude Code plugin (PostToolUse hook that records
// which session spawned which) and backfills links from existing transcripts.
func setup(args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	source := fs.String("source", defaultPluginSource, "marketplace source: owner/repo, git or https URL, or a local path")
	noPlugin := fs.Bool("no-plugin", false, "skip installing the plugin")
	noBackfill := fs.Bool("no-backfill", false, "skip scanning existing transcripts")
	if fs.Parse(args) != nil {
		return 2
	}
	ok := true

	if _, err := exec.LookPath("regia"); err != nil {
		fmt.Println("! regia is not on PATH: the hook runs `regia hook spawn` and will do nothing until it is.")
		fmt.Println("  install it with: go install github.com/farzeni/regia/cmd/regia@main (and put $(go env GOPATH)/bin on PATH)")
		ok = false
	}

	if !*noPlugin {
		fmt.Printf("→ claude plugin install regia --marketplace %s\n", *source)
		c := exec.Command("claude", "plugin", "install", "regia", "--marketplace", *source)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			fmt.Printf("! plugin install failed (%v)\n", err)
			ok = false
		} else {
			fmt.Println("  new sessions record their spawns; running ones after a restart or /reload-plugins")
		}
	}

	if !*noBackfill {
		n, err := spawns.Backfill()
		if err != nil {
			fmt.Printf("! backfill: %v\n", err)
			ok = false
		} else {
			fmt.Printf("→ backfill: %d spawn links found in existing transcripts (%s)\n", n, spawns.Path())
		}
	}
	if !ok {
		return 1
	}
	return 0
}
