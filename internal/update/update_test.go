package update

import (
	"context"
	"errors"
	"testing"
	"time"
)

const (
	old   = "v0.0.0-20261114100000-aaaaaaaaaaaa"
	fresh = "v0.0.0-20261114161559-a6f467d80c5b"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		latest, cur string
		want        bool
	}{
		{fresh, old, true},
		{old, fresh, false}, // never downgrade
		{fresh, fresh, false},
		{"v0.1.0", fresh, true}, // first tag beats any v0.0.0 pseudo-version
		{fresh, "v0.1.0", false},
		{fresh, "(devel)", true},
		{"garbage", old, false},
	} {
		if got := Newer(c.latest, c.cur); got != c.want {
			t.Errorf("Newer(%s, %s) = %v, want %v", c.latest, c.cur, got, c.want)
		}
	}
}

func TestDevBuildString(t *testing.T) {
	if s := (Build{Dev: true, Revision: "6696f6c80c5b", Dirty: true}).String(); s != "dev build 6696f6c+changes" {
		t.Fatal(s)
	}
	if s := (Build{Dev: true}).String(); s != "dev build" {
		t.Fatalf("%q", s)
	}
}

// fake wires Auto to an installed build at version cur, a proxy reporting
// pub, and counts what it would run.
type fake struct{ asked, installed, plugin int }

func stub(t *testing.T, b Build, pub string, installErr error) *fake {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("REGIA_NO_AUTOUPDATE", "")
	f := &fake{}
	currentFn = func() Build { return b }
	latestFn = func(context.Context) (string, error) { f.asked++; return pub, nil }
	installFn = func(_ context.Context, v string) (string, error) { f.installed++; return "/x/regia", installErr }
	pluginFn = func(context.Context) (string, error) { f.plugin++; return "", nil }
	t.Cleanup(func() { currentFn, latestFn, installFn, pluginFn = Current, Latest, Install, UpdatePlugin })
	return f
}

func TestAutoInstallsNewerOncePerDay(t *testing.T) {
	f := stub(t, Build{Version: old}, fresh, nil)
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	r, ok := Auto(context.Background(), now)
	if !ok || r.Err != nil || r.From != old || r.To != fresh || f.installed != 1 || f.plugin != 1 {
		t.Fatalf("first run: %+v ok=%v %+v", r, ok, f)
	}
	if _, ok := Auto(context.Background(), now.Add(23*time.Hour)); ok || f.asked != 1 {
		t.Fatalf("checked again within a day: asked %d", f.asked)
	}
	Auto(context.Background(), now.Add(25*time.Hour))
	if f.asked != 2 {
		t.Fatalf("no check after a day: asked %d", f.asked)
	}
}

func TestAutoSkips(t *testing.T) {
	now := time.Now()
	t.Run("dev build", func(t *testing.T) {
		f := stub(t, Build{Version: "(devel)", Revision: "abc", Dev: true}, fresh, nil)
		if _, ok := Auto(context.Background(), now); ok || f.asked != 0 {
			t.Fatalf("dev builds must not be touched: %+v", f)
		}
	})
	t.Run("up to date", func(t *testing.T) {
		f := stub(t, Build{Version: fresh}, fresh, nil)
		if _, ok := Auto(context.Background(), now); ok || f.installed != 0 {
			t.Fatalf("%+v", f)
		}
	})
	t.Run("published is older", func(t *testing.T) {
		f := stub(t, Build{Version: fresh}, old, nil)
		if _, ok := Auto(context.Background(), now); ok || f.installed != 0 {
			t.Fatalf("would downgrade: %+v", f)
		}
	})
	t.Run("turned off", func(t *testing.T) {
		f := stub(t, Build{Version: old}, fresh, nil)
		off := false
		if err := (Settings{AutoUpdate: &off}).Save(); err != nil {
			t.Fatal(err)
		}
		if _, ok := Auto(context.Background(), now); ok || f.asked != 0 {
			t.Fatalf("%+v", f)
		}
	})
	t.Run("env off", func(t *testing.T) {
		f := stub(t, Build{Version: old}, fresh, nil)
		t.Setenv("REGIA_NO_AUTOUPDATE", "1")
		if _, ok := Auto(context.Background(), now); ok || f.asked != 0 {
			t.Fatalf("%+v", f)
		}
	})
	t.Run("install fails", func(t *testing.T) {
		f := stub(t, Build{Version: old}, fresh, errors.New("no network"))
		r, ok := Auto(context.Background(), now)
		if !ok || r.Err == nil || f.plugin != 0 {
			t.Fatalf("%+v %+v", r, f)
		}
	})
}
