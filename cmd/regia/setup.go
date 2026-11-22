package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/farzeni/regia/internal/spawns"
	"github.com/farzeni/regia/internal/update"
)

// defaultPluginSource is where `regia setup` installs the plugin (the hook)
// from. The repo is public, so https needs no keys on a new machine.
const defaultPluginSource = "https://github.com/farzeni/regia.git"

// setup installs the regia Claude Code plugin (its hooks, the regia MCP server
// and the regia:organize skill) and backfills links from existing transcripts.
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
			// install leaves an older copy as it is: bring it to the latest
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			_ = exec.CommandContext(ctx, "claude", "plugin", "marketplace", "update", "regia").Run()
			if out, err := update.UpdatePlugin(ctx); err != nil {
				fmt.Printf("! plugin update failed (%v): %s\n", err, out)
			}
			cancel()
			fmt.Println("  the plugin gives every session:")
			fmt.Println("  - hooks: which agent started which, and group awareness (regia context)")
			fmt.Println("  - the regia MCP server (regia mcp): agents read the tree; coordinators, and agents you grant with O, organize it")
			fmt.Println("  - the regia:organize skill: how agents use those tools")
			fmt.Println("  new sessions get them; running ones after a restart or /reload-plugins")
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
