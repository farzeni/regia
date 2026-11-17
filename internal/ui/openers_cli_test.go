package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOpenersCLI(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PAGER", "")
	var b strings.Builder
	if err := Openers([]string{"list"}, &b); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"default ", "vscode ", "nvim-tmux ", "idea "} {
		if !strings.Contains(b.String(), p) {
			t.Fatalf("list misses %q:\n%s", p, b.String())
		}
	}

	conf := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "regia", "openers.conf")
	os.MkdirAll(filepath.Dir(conf), 0o755)
	os.WriteFile(conf, []byte("preset = vscode\nurl = firefox {url}\n"), 0o644)
	b.Reset()
	Openers([]string{"show"}, &b)
	for _, want := range []string{"preset = vscode", "code -g {path}:{line}", "# preset vscode", "firefox {url}", "openers.conf:2"} {
		if !strings.Contains(b.String(), want) {
			t.Fatalf("show misses %q:\n%s", want, b.String())
		}
	}

	var got []string
	orig := newCmd
	defer func() { newCmd = orig }()
	newCmd = func(name string, args ...string) *exec.Cmd {
		got = append([]string{name}, args...)
		return exec.Command("true")
	}
	b.Reset()
	if err := Openers([]string{"test", "file", "/a b/x.go:12"}, &b); err != nil {
		t.Fatal(err)
	}
	if want := []string{"code", "-g", "/a b/x.go:12"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("test file ran %q, want %q (%s)", got, want, b.String())
	}

	os.WriteFile(conf, []byte("fiel = vim\n"), 0o644)
	b.Reset()
	Openers([]string{"show"}, &b)
	if !strings.Contains(b.String(), `openers.conf:1: unknown key "fiel"`) {
		t.Fatalf("show should explain the mistake:\n%s", b.String())
	}
	if err := Openers([]string{"test", "file", "x"}, &b); err == nil {
		t.Fatal("test should refuse a broken config")
	}
}
