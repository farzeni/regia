package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/farzeni/regia/internal/update"
)

const (
	old   = "v0.0.0-20261114100000-aaaaaaaaaaaa"
	fresh = "v0.0.0-20261114161559-a6f467d80c5b"
)

type calls struct{ installed, plugin int }

// stubCLI: the running build is b, the proxy says pub; nothing real runs.
func stubCLI(t *testing.T, b update.Build, pub string) (*calls, *bytes.Buffer) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	c, out := &calls{}, &bytes.Buffer{}
	current = func() update.Build { return b }
	latest = func(context.Context) (string, error) { return pub, nil }
	install = func(context.Context, string) (string, error) { c.installed++; return "/home/u/go/bin/regia", nil }
	updatePlugin = func(context.Context) (string, error) { c.plugin++; return "", nil }
	stdout = out
	t.Cleanup(func() {
		current, latest, install, updatePlugin = update.Current, update.Latest, update.Install, update.UpdatePlugin
		stdout = nil
	})
	return c, out
}

func TestUpdateInstallsNewer(t *testing.T) {
	c, out := stubCLI(t, update.Build{Version: old}, fresh)
	if err := dispatch([]string{"update"}); err != nil {
		t.Fatal(err)
	}
	if c.installed != 1 || c.plugin != 1 || !strings.Contains(out.String(), "installed "+fresh) {
		t.Fatalf("%+v\n%s", c, out)
	}
}

func TestUpdateUpToDateStillUpdatesPlugin(t *testing.T) {
	c, out := stubCLI(t, update.Build{Version: fresh}, fresh)
	if err := dispatch([]string{"update"}); err != nil {
		t.Fatal(err)
	}
	if c.installed != 0 || c.plugin != 1 || !strings.Contains(out.String(), "up to date") {
		t.Fatalf("%+v\n%s", c, out)
	}
}

func TestUpdateRefusesDevBuild(t *testing.T) {
	dev := update.Build{Version: "(devel)", Revision: "6696f6c80c5b", Dev: true}
	c, _ := stubCLI(t, dev, fresh)
	err := dispatch([]string{"update"})
	if err == nil || !strings.Contains(err.Error(), "--force") || c.installed != 0 || c.plugin != 0 {
		t.Fatalf("dev build replaced without --force: %v %+v", err, c)
	}
	if err := dispatch([]string{"update", "--force"}); err != nil || c.installed != 1 {
		t.Fatalf("--force: %v %+v", err, c)
	}
}

func TestUpdateCheckInstallsNothing(t *testing.T) {
	c, out := stubCLI(t, update.Build{Version: old}, fresh)
	if err := dispatch([]string{"update", "--check"}); err != nil {
		t.Fatal(err)
	}
	if c.installed+c.plugin != 0 || !strings.Contains(out.String(), "published:  "+fresh) {
		t.Fatalf("%+v\n%s", c, out)
	}
}

func TestUpdateAutoOff(t *testing.T) {
	stubCLI(t, update.Build{Version: old}, fresh)
	t.Setenv("REGIA_NO_AUTOUPDATE", "")
	if err := dispatch([]string{"update", "--auto", "off"}); err != nil || update.AutoEnabled() {
		t.Fatalf("still on: %v", err)
	}
	if err := dispatch([]string{"update", "--auto=on"}); err != nil || !update.AutoEnabled() {
		t.Fatalf("still off: %v", err)
	}
	if err := dispatch([]string{"update", "--bogus"}); err == nil {
		t.Fatal("bad flag accepted")
	}
}

func TestUninstallNeedsYes(t *testing.T) {
	_, out := stubCLI(t, update.Build{Version: old}, fresh)
	stdin = strings.NewReader("\n")
	t.Cleanup(func() { stdin = nil })
	if err := dispatch([]string{"uninstall", "--purge"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "nothing removed") || strings.Contains(out.String(), "plugin removed") {
		t.Fatalf("%s", out)
	}
}
