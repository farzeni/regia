package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/farzeni/regia/internal/update"
)

// Seams for tests.
var (
	current                = update.Current
	latest                 = update.Latest
	install                = update.Install
	updatePlugin           = update.UpdatePlugin
	stdout       io.Writer = os.Stdout
	stdin        io.Reader = os.Stdin
)

const updateUsage = `usage: regia update [--check] [--force] [--auto on|off]

  regia update            install the latest regia and update the Claude Code plugin
  regia update --check    only say whether a newer version exists
  regia update --force    also replace a dev build (made from a checkout)
  regia update --auto off turn off the daily automatic update (on by default)`

func cmdUpdate(args []string) error {
	var check, force bool
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--check":
			check = true
		case a == "--force":
			force = true
		case a == "--auto" && i+1 < len(args):
			i++
			return setAuto(args[i])
		case strings.HasPrefix(a, "--auto="):
			return setAuto(strings.TrimPrefix(a, "--auto="))
		default:
			return fmt.Errorf("%s", updateUsage)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	b := current()
	fmt.Fprintf(stdout, "this regia: %s\n", b)
	l, err := latest(ctx)
	if err != nil {
		return err
	}
	newer := update.Newer(l, b.Version)
	if check {
		switch {
		case b.Dev:
			fmt.Fprintf(stdout, "published:  %s (this is a dev build; regia update --force replaces it)\n", l)
		case newer:
			fmt.Fprintf(stdout, "published:  %s — regia update installs it\n", l)
		default:
			fmt.Fprintln(stdout, "up to date")
		}
		return nil
	}
	switch {
	case b.Dev && !force:
		return fmt.Errorf("this is a dev build from a checkout, possibly ahead of the published %s:\n"+
			"rebuild it from the checkout (git pull && go build -o %s ./cmd/regia),\n"+
			"or replace it with the published version: regia update --force", l, exeOr("regia"))
	case !newer && !b.Dev:
		fmt.Fprintln(stdout, "regia is up to date")
	default:
		fmt.Fprintf(stdout, "installing %s…\n", l)
		exe, err := install(ctx, l)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "installed %s at %s\n", l, exe)
	}
	// The plugin carries the hook; keep it in step even when the binary was current.
	if out, err := updatePlugin(ctx); err != nil {
		fmt.Fprintf(stdout, "plugin: not updated (%v) %s\n  run regia setup if it was never installed\n", err, out)
	} else {
		fmt.Fprintln(stdout, "plugin: up to date")
	}
	return nil
}

func setAuto(v string) error {
	var on bool
	switch v {
	case "on", "true", "1":
		on = true
	case "off", "false", "0":
	default:
		return fmt.Errorf("regia update --auto on|off")
	}
	s := update.LoadSettings()
	s.AutoUpdate = &on
	if err := s.Save(); err != nil {
		return err
	}
	if on {
		fmt.Fprintln(stdout, "automatic updates on: regia checks once a day when it opens")
	} else {
		fmt.Fprintln(stdout, "automatic updates off: run regia update when you want one")
	}
	return nil
}

func cmdVersion() {
	b := current()
	fmt.Fprintln(stdout, "regia", b)
	if !b.Dev && !update.AutoEnabled() {
		fmt.Fprintln(stdout, "automatic updates off")
	}
}

func exeOr(def string) string {
	if exe, err := update.Executable(); err == nil {
		return exe
	}
	return def
}

const uninstallUsage = `usage: regia uninstall [--purge] [--yes]

  removes the Claude Code plugin and the regia binary;
  --purge also deletes your layout (groups, archive) in ~/.config/regia and the update cache`

func cmdUninstall(args []string) error {
	var purge, yes bool
	for _, a := range args {
		switch a {
		case "--purge":
			purge = true
		case "--yes", "-y":
			yes = true
		default:
			return fmt.Errorf("%s", uninstallUsage)
		}
	}
	exe, err := update.Executable()
	if err != nil {
		return err
	}
	cfg, _ := os.UserConfigDir()
	cache, _ := os.UserCacheDir()
	cfg, cache = filepath.Join(cfg, "regia"), filepath.Join(cache, "regia")

	fmt.Fprintln(stdout, "this removes:")
	fmt.Fprintln(stdout, "  the Claude Code plugin", update.Plugin)
	fmt.Fprintln(stdout, "  the binary", exe)
	if purge {
		fmt.Fprintln(stdout, "  your layout and settings", cfg)
		fmt.Fprintln(stdout, "  the update cache", cache)
	} else {
		fmt.Fprintln(stdout, "and keeps your layout in", cfg, "(--purge deletes it)")
	}
	if !yes {
		fmt.Fprint(stdout, "go ahead? [y/N] ")
		line, _ := bufio.NewReader(stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			fmt.Fprintln(stdout, "nothing removed")
			return nil
		}
	}
	if out, err := exec.Command("claude", "plugin", "uninstall", update.Plugin).CombinedOutput(); err != nil {
		fmt.Fprintf(stdout, "plugin: %v %s (continuing)\n", err, strings.TrimSpace(string(out)))
	} else {
		fmt.Fprintln(stdout, "plugin removed")
	}
	if purge {
		for _, d := range []string{cfg, cache} {
			if err := os.RemoveAll(d); err != nil {
				return err
			}
		}
		fmt.Fprintln(stdout, "layout and cache removed")
	}
	if err := os.Remove(exe); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "binary removed — regia is uninstalled")
	return nil
}
