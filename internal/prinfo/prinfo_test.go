package prinfo

import (
	"context"
	"errors"
	"testing"
)

func TestParse(t *testing.T) {
	i := Parse([]byte(`{"number":12,"url":"u","state":"OPEN","isDraft":false,"reviewDecision":"CHANGES_REQUESTED",
	 "statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"},{"status":"COMPLETED","conclusion":"FAILURE"}]}`))
	if !i.OK || i.State != "open" || i.Checks != "failing" || i.Review != "changes" || !i.NeedsLook() {
		t.Fatalf("%+v", i)
	}
	if i.Reason() != "PR #12: CI failing, changes requested" {
		t.Fatal(i.Reason())
	}
	i = Parse([]byte(`{"number":3,"state":"OPEN","isDraft":true,"statusCheckRollup":[{"status":"IN_PROGRESS"},{"state":"SUCCESS"}]}`))
	if i.State != "draft" || i.Checks != "pending" || i.NeedsLook() {
		t.Fatalf("%+v", i)
	}
	i = Parse([]byte(`{"number":4,"state":"MERGED","reviewDecision":"APPROVED","statusCheckRollup":[{"state":"FAILURE"}]}`))
	if i.State != "merged" || !i.Final() || i.NeedsLook() || i.Reason() != "" {
		t.Fatalf("%+v", i)
	}
	if Parse([]byte(`garbage`)).OK || Parse([]byte(`{}`)).OK {
		t.Fatal("bad input should be not OK")
	}
}

func TestMergedNotRefetched(t *testing.T) {
	ghBroken = false
	calls := 0
	lookPath = func(string) (string, error) { return "gh", nil }
	run = func(context.Context, string, ...string) ([]byte, error) {
		calls++
		return []byte(`{"number":7,"state":"MERGED"}`), nil
	}
	Fetch("/d", "b")
	if _, due := Cached("/d", "b"); due || calls != 1 {
		t.Fatalf("merged PR must not be refetched (calls=%d)", calls)
	}
}

func TestRefresherOnce(t *testing.T) {
	ghBroken = false
	lookPath = func(string) (string, error) { return "gh", nil }
	run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`{"number":9,"state":"OPEN"}`), nil
	}
	f := Refresher("/r", "feat")
	if f == nil || Refresher("/r", "feat") != nil {
		t.Fatal("first call must return work, the second (in flight) nil")
	}
	f()
	if i, due := Cached("/r", "feat"); !i.OK || i.Number != 9 || due {
		t.Fatalf("%+v due=%v", i, due)
	}
	if Refresher("/r", "feat") != nil {
		t.Fatal("fresh open PR must wait for the TTL")
	}
	if Refresher("", "x") != nil || Refresher("/r", "") != nil {
		t.Fatal("empty dir/ref: nothing to do")
	}
}

func TestGhMissingIsSilent(t *testing.T) {
	ghBroken = false
	lookPath = func(string) (string, error) { return "", errors.New("no gh") }
	if Fetch("/x", "y").OK {
		t.Fatal("expected empty")
	}
	if _, due := Cached("/x", "z"); due {
		t.Fatal("gh missing: nothing should be due")
	}
	ghBroken = false
}
