package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/farzeni/regia/internal/plugins"
	"github.com/farzeni/regia/internal/ui"
)

const pluginUsage = `regia plugin — your own scripts that add to the agent panel (docs/plugins.md)

  regia plugin list                       the plugins found, what they apply to, broken manifests
  regia plugin new <name>                 a working skeleton in the plugins directory to start from
  regia plugin input <plugin> <agent>     the JSON the plugin reads on stdin for that agent
  regia plugin run <plugin> <agent> [--action <id>]
                                          run it once for that agent and check what it prints

  <plugin> is a name in the plugins directory, or a path to a plugin directory.
  <agent> is an id from regia --dump (a prefix is enough). Plugins live in
  %s (REGIA_PLUGINS_DIR overrides).`

func cmdPlugin(args []string, w io.Writer) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintf(w, pluginUsage+"\n", plugins.Dir())
		return nil
	}
	switch args[0] {
	case "list", "ls":
		return pluginList(w)
	case "new":
		if len(args) != 2 {
			return fmt.Errorf("usage: regia plugin new <name>")
		}
		return pluginNew(args[1], w)
	case "input":
		if len(args) != 3 {
			return fmt.Errorf("usage: regia plugin input <plugin> <agent>")
		}
		p, err := findPlugin(args[1])
		if err != nil {
			return err
		}
		in, dir, applies, err := ui.PluginInput(args[2], p)
		if err != nil {
			return err
		}
		if !applies {
			fmt.Fprintf(os.Stderr, "note: %s's dirs/states/interactive don't match this agent: regia wouldn't run it for it\n", p.Name)
		}
		fmt.Fprintf(os.Stderr, "runs in: %s\n", dir)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(in)
	case "run":
		return pluginRun(args[1:], w)
	}
	return fmt.Errorf("unknown: regia plugin %s\n\n"+pluginUsage, args[0], plugins.Dir())
}

func findPlugin(name string) (*plugins.Manifest, error) {
	dir := filepath.Join(plugins.Dir(), name)
	if st, err := os.Stat(name); err == nil && st.IsDir() && strings.ContainsRune(name, os.PathSeparator) {
		dir = name // a path works too, for a plugin not installed yet
	}
	p, err := plugins.LoadOne(dir)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("no plugin %q: expected %s", name, filepath.Join(dir, "plugin.json"))
	}
	return p, err
}

func pluginList(w io.Writer) error {
	dir := plugins.Dir()
	fmt.Fprintf(w, "plugins in %s\n", dir)
	ps, errs := plugins.Load(dir)
	var disabled []string
	all, _ := os.ReadDir(dir)
	for _, e := range all {
		if q, err := plugins.LoadOne(filepath.Join(dir, e.Name())); err == nil && q.Disabled {
			disabled = append(disabled, q.Name)
		}
	}
	if len(ps)+len(errs)+len(disabled) == 0 {
		fmt.Fprintln(w, "  (none: regia plugin new <name> starts one)")
	}
	for _, p := range ps {
		scope := "every agent"
		if len(p.Dirs) > 0 {
			scope = "under " + strings.Join(p.Dirs, ", ")
		}
		if len(p.States) > 0 {
			scope += " · " + strings.Join(p.States, "/")
		}
		fmt.Fprintf(w, "  ✓ %-16s %s · %s\n", p.Name, strings.Join(p.Command, " "), scope)
	}
	for _, n := range disabled {
		fmt.Fprintf(w, "  – %-16s disabled\n", n)
	}
	for _, e := range errs {
		fmt.Fprintf(w, "  ✗ %-16s %v\n", e.Name, e.Err)
	}
	return nil
}

func pluginRun(args []string, w io.Writer) error {
	var pos []string
	action := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--action" && i+1 < len(args) {
			action = args[i+1]
			i++
			continue
		}
		pos = append(pos, args[i])
	}
	if len(pos) != 2 {
		return fmt.Errorf("usage: regia plugin run <plugin> <agent> [--action <id>]")
	}
	p, err := findPlugin(pos[0])
	if err != nil {
		return err
	}
	in, dir, applies, err := ui.PluginInput(pos[1], p)
	if err != nil {
		return err
	}
	if !applies {
		fmt.Fprintln(w, "note: dirs/states/interactive in plugin.json don't match this agent: regia wouldn't run it; running anyway")
	}
	what := "event refresh"
	if action != "" {
		in.Event, in.Action = "action", action
		what = "event action, action " + action
	}
	fmt.Fprintf(w, "running %s (%s) in %s\n", strings.Join(p.Command, " "), what, dir)
	out, stderr, err := plugins.Run(context.Background(), p, in, dir)
	if stderr != "" {
		fmt.Fprintf(w, "\nstderr:\n%s\n", indent(stderr))
	}
	if _, warning := err.(*plugins.Warning); err != nil && !warning {
		return fmt.Errorf("failed: %v", err)
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Fprintf(w, "\noutput, as regia reads it:\n%s\n", indent(string(b)))
	if err != nil {
		fmt.Fprintf(w, "\n⚠ problems (regia shows them dim on the card): %v\n", err)
		return nil
	}
	fmt.Fprintln(w, "\n✓ ok")
	return nil
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n  ")
}

const skeletonManifest = `{
  "command": ["./check.sh"],
  "dirs": [],
  "every": "2m",
  "timeout": "10s"
}
`

const skeletonScript = `#!/usr/bin/env bash
# A regia plugin: reads the agent as JSON on stdin, prints JSON on stdout.
# Protocol: docs/plugins.md in the regia repo. Test: regia plugin run %[1]s <agent-id>
set -euo pipefail

input=$(cat)
event=$(jq -r .event <<<"$input")
branch=$(jq -r '.agent.branch // ""' <<<"$input")

if [ "$event" = action ]; then
  case "$(jq -r .action <<<"$input")" in
    hello) jq -n --arg b "$branch" '{message: ("hello from " + $b)}' ;;
  esac
  exit 0
fi

jq -n --arg b "$branch" '{
  fields: [{label: "%[1]s", text: ("branch " + $b), status: "ok"}],
  actions: [{id: "hello", label: "say hello"}]
}'
`

func pluginNew(name string, w io.Writer) error {
	if name == "" || strings.ContainsAny(name, "/\\ ") || strings.HasPrefix(name, ".") {
		return fmt.Errorf("plugin name %q: one word, no slashes or spaces", name)
	}
	dir := filepath.Join(plugins.Dir(), name)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("%s already exists", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(skeletonManifest), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "check.sh"), []byte(fmt.Sprintf(skeletonScript, name)), 0o755); err != nil {
		return err
	}
	fmt.Fprintf(w, "created %s (plugin.json, check.sh; needs jq)\ntry it: regia plugin run %s <agent-id>\nregia picks it up within a few seconds, no restart\n", dir, name)
	return nil
}
