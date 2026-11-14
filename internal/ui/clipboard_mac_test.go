package ui

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeMac makes the clipboard code take the macOS path, with osascript
// answering from the given pasteboard: its type identifiers, a PNG (what the
// conversion script would return) and file URLs.
func fakeMac(t *testing.T, utis []string, png []byte, urls []string) *[]string {
	t.Helper()
	og, oj := goos, runJXA
	t.Cleanup(func() { goos, runJXA = og, oj })
	goos = "darwin"
	var ran []string
	runJXA = func(script string) (string, error) {
		switch script {
		case jxaTypes:
			ran = append(ran, "types")
			b, _ := jsonArray(utis)
			return b, nil
		case jxaPNG:
			ran = append(ran, "png")
			if png == nil {
				return "", nil
			}
			return base64.StdEncoding.EncodeToString(png), nil
		case jxaFiles:
			ran = append(ran, "files")
			return strings.Join(urls, "\n"), nil
		}
		t.Fatalf("unexpected script %q", script)
		return "", nil
	}
	return &ran
}

func jsonArray(ss []string) (string, error) {
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = `"` + s + `"`
	}
	return "[" + strings.Join(q, ",") + "]", nil
}

func TestMacClipboardTypes(t *testing.T) {
	fakeMac(t, []string{"public.png", "public.tiff", "public.file-url", "public.utf8-plain-text", "com.apple.something"}, nil, nil)
	if got := clipboardTypes(); !slices.Equal(got, []string{"image/png", "image/tiff", "text/uri-list", "text/plain"}) {
		t.Fatalf("types %v", got)
	}
}

func TestMacCtrlVScreenshot(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nscreenshot")
	ran := fakeMac(t, []string{"public.png", "public.tiff"}, png, nil)
	m := testModel(t)
	got := stubSpawn(t)
	typeAndRender(m, "what's wrong here ")
	send(m, tea.KeyMsg{Type: tea.KeyCtrlV})
	if m.draft() != "what's wrong here [Image #1]" {
		t.Fatalf("draft %q (ran %v)", m.draft(), *ran)
	}
	send(m, tkey("enter"))
	p := strings.TrimPrefix(*got, "what's wrong here ")
	if b, err := os.ReadFile(p); err != nil || string(b) != string(png) || filepath.Ext(p) != ".png" {
		t.Fatalf("saved %q: %v %q", p, err, b)
	}
}

// An image some apps only offer as TIFF is converted by the script and saved
// as png.
func TestMacCtrlVTiffOnly(t *testing.T) {
	fakeMac(t, []string{"public.tiff"}, []byte("\x89PNGconverted"), nil)
	m := testModel(t)
	send(m, tea.KeyMsg{Type: tea.KeyCtrlV})
	p := m.pastes["[Image #1]"]
	if filepath.Ext(p) != ".png" {
		t.Fatalf("draft %q, saved %q", m.draft(), p)
	}
}

// Finder's cmd+c puts the file's icon on the clipboard as an image too: the
// file wins, not its icon.
func TestMacCtrlVFinderFiles(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "Screen Shot 1.png")
	doc := filepath.Join(dir, "notes.txt")
	os.WriteFile(img, []byte("x"), 0o600)
	os.WriteFile(doc, []byte("x"), 0o600)
	ran := fakeMac(t, []string{"public.file-url", "public.tiff"}, []byte("icon"),
		[]string{"file://" + strings.ReplaceAll(img, " ", "%20"), "file://" + doc})
	m := testModel(t)
	send(m, tea.KeyMsg{Type: tea.KeyCtrlV})
	if m.draft() != "[Image #1] "+doc || m.pastes["[Image #1]"] != img {
		t.Fatalf("draft %q pastes %v", m.draft(), m.pastes)
	}
	if slices.Contains(*ran, "png") {
		t.Fatalf("read the icon: %v", *ran)
	}
}

// Plain text on the clipboard: no image or file, pbpaste's text goes in.
func TestMacCtrlVText(t *testing.T) {
	fakeMac(t, []string{"public.utf8-plain-text"}, nil, nil)
	orig := readClipboard
	t.Cleanup(func() { readClipboard = orig })
	readClipboard = func() (string, error) { return "hello", nil }
	m := testModel(t)
	send(m, tea.KeyMsg{Type: tea.KeyCtrlV})
	if m.draft() != "hello" {
		t.Fatalf("draft %q", m.draft())
	}
}

// Dropped on Terminal.app or iTerm, a path comes with backslash-escaped spaces.
func TestMacDroppedPath(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "Screen Shot 2026.png")
	os.WriteFile(img, []byte("x"), 0o600)
	m := testModel(t)
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.ReplaceAll(img, " ", `\ `) + " "), Paste: true})
	if m.draft() != "[Image #1]" || m.pastes["[Image #1]"] != img {
		t.Fatalf("draft %q pastes %v", m.draft(), m.pastes)
	}
}
