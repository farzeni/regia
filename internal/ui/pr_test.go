package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/prinfo"
)

func TestPRRef(t *testing.T) {
	dir := t.TempDir()
	a := &claude.Agent{Cwd: dir, Branch: "feat-x"}
	if d, r := prRef(a); d != dir || r != "feat-x" {
		t.Fatalf("branch ref: %q %q", d, r)
	}
	a.PRs = []claude.Link{{ID: "1", Kind: "shell"}, {ID: "7", Href: "https://github.com/o/r/pull/7", Kind: "pr"}}
	if _, r := prRef(a); r != "https://github.com/o/r/pull/7" {
		t.Fatalf("PR link wins over the branch: %q", r)
	}
	if _, r := prRef(&claude.Agent{Cwd: dir, Branch: "main"}); r != "" {
		t.Fatal("the default branch has no PR")
	}
	if _, r := prRef(&claude.Agent{Cwd: dir + "/gone", Branch: "feat"}); r != "" {
		t.Fatal("a removed directory can't be asked by branch")
	}
}

func TestPRLine(t *testing.T) {
	l, _ := prLine(prinfo.Info{OK: true, Number: 12, State: "open", Checks: "failing", Review: "changes"})
	if l != "#12 · open · CI ✗ failing · changes requested" {
		t.Fatal(l)
	}
	if l, _ := prLine(prinfo.Info{OK: true, Number: 4, State: "merged", Checks: "failing"}); !strings.Contains(l, "✓ merged") || strings.Contains(l, "CI") {
		t.Fatal(l)
	}
	if l, _ := prLine(prinfo.Info{}); l != "" {
		t.Fatal("unknown PR: no line")
	}
}

// A done (and already seen, so hidden) agent whose PR fails CI comes back
// in the inbox with the reason, and its card says so.
func TestPRFailingPutsDoneAgentInInbox(t *testing.T) {
	f := newReady(t)
	m := f.m
	a := m.agents["done0001"]
	a.Cwd, a.Branch = t.TempDir(), "feat-ci"
	now := time.Now()
	inInbox := func() bool {
		for _, r := range m.inboxRows(now) {
			if r.ID == "done0001" {
				return true
			}
		}
		return false
	}
	if inInbox() {
		t.Fatal("seen done agent without a PR problem is not in the inbox")
	}
	prinfo.Seed(a.Cwd, "feat-ci", prinfo.Info{OK: true, Number: 31, State: "open", Checks: "failing"})
	if !inInbox() {
		t.Fatal("failing CI should bring it into the inbox")
	}
	r := m.inboxRow(a, 0)
	if s := ansi.Strip(m.inboxSubline(r, 120)); !strings.Contains(s, "PR #31: CI failing") {
		t.Fatalf("subline %q", s)
	}
	m.layout.Archive("done0001")
	if inInbox() {
		t.Fatal("archived agents stay out")
	}
	prinfo.Seed(a.Cwd, "feat-ci", prinfo.Info{OK: true, Number: 31, State: "merged", Checks: "failing"})
	if prNeedsLook(a) {
		t.Fatal("a merged PR needs nothing")
	}
}
