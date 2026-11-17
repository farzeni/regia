package ui

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/farzeni/regia/internal/claude"
)

// copyText is y in the conversation: the system clipboard, or, with no tool
// for it (an ssh session, a bare system), the terminal's own clipboard via
// OSC 52. A variable for tests.
var copyText = func(s string) error {
	err := copyClipboard(s)
	if errors.Is(err, os.ErrNotExist) {
		_, err = fmt.Fprint(os.Stdout, osc52(s))
	}
	return err
}

// osc52 asks the terminal to put s on the clipboard.
func osc52(s string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(s)) + "\x07"
}

func copyClipboard(s string) error {
	for _, c := range [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"pbcopy"}} {
		if _, err := exec.LookPath(c[0]); err == nil {
			cmd := exec.Command(c[0], c[1:]...)
			cmd.Stdin = bytes.NewBufferString(s)
			return cmd.Run()
		}
	}
	return os.ErrNotExist
}

var readClipboard = func() (string, error) {
	for _, c := range [][]string{{"wl-paste", "--no-newline"}, {"xclip", "-selection", "clipboard", "-o"}, {"pbpaste"}} {
		if _, err := exec.LookPath(c[0]); err == nil {
			out, err := exec.Command(c[0], c[1:]...).Output()
			return string(out), err
		}
	}
	return "", os.ErrNotExist
}

// clipboardTypes lists the MIME types the clipboard offers (nil when it
// can't tell, e.g. no wl-paste/xclip).
var clipboardTypes = func() []string {
	if goos == "darwin" {
		return macClipboardTypes()
	}
	for _, c := range [][]string{{"wl-paste", "--list-types"}, {"xclip", "-selection", "clipboard", "-t", "TARGETS", "-o"}} {
		if _, err := exec.LookPath(c[0]); err == nil {
			out, err := exec.Command(c[0], c[1:]...).Output()
			if err != nil {
				return nil
			}
			return strings.Fields(string(out))
		}
	}
	return nil
}

// readClipboardAs reads the clipboard in one MIME type (an image, a uri-list).
var readClipboardAs = func(mime string) ([]byte, error) {
	if goos == "darwin" {
		return macReadClipboardAs(mime)
	}
	for _, c := range [][]string{{"wl-paste", "--no-newline", "--type", mime}, {"xclip", "-selection", "clipboard", "-t", mime, "-o"}} {
		if _, err := exec.LookPath(c[0]); err == nil {
			return exec.Command(c[0], c[1:]...).Output()
		}
	}
	return nil, os.ErrNotExist
}

// clipboardImage returns the image on the clipboard, if any, and its extension.
func clipboardImage(types []string) (data []byte, ext string, err error) {
	for _, pref := range []string{"image/png", "image/jpeg", "image/webp", "image/gif"} {
		for _, t := range types {
			if t == pref {
				b, err := readClipboardAs(t)
				ext = "." + strings.TrimPrefix(t, "image/")
				if ext == ".jpeg" {
					ext = ".jpg"
				}
				return b, ext, err
			}
		}
	}
	for _, t := range types {
		if strings.HasPrefix(t, "image/") {
			b, err := readClipboardAs("image/png") // wl-paste/xclip convert to png
			return b, ".png", err
		}
	}
	return nil, "", nil
}

// clipboardFiles returns the local files on the clipboard (copied in a file
// manager: text/uri-list). A uri-list of web links gives none.
func clipboardFiles(types []string) []string {
	for _, t := range types {
		if t == "text/uri-list" || t == "x-special/gnome-copied-files" {
			b, err := readClipboardAs(t)
			if err != nil {
				return nil
			}
			return uriPaths(string(b))
		}
	}
	return nil
}

// uriPaths reads file paths from a uri-list (or gnome's copied-files, whose
// first line is "copy"/"cut"), skipping anything that is not a local file.
func uriPaths(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "file://") {
			continue
		}
		if u, err := url.Parse(l); err == nil && u.Path != "" {
			out = append(out, u.Path)
		}
	}
	return out
}

// savePastedImage writes a pasted image under ~/.cache/regia/pastes so the
// agent can read it, and returns its path.
func savePastedImage(data []byte, ext string) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "regia", "pastes")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	p := filepath.Join(dir, time.Now().Format("20060102-150405.000")+ext)
	return p, os.WriteFile(p, data, 0o600)
}

var imageExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}

// isImage says whether p is an existing image file.
func isImage(p string) bool {
	if !imageExt[strings.ToLower(filepath.Ext(p))] {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// droppedPaths says whether a pasted text is just one or more existing file
// paths (dropped on the terminal: maybe quoted, backslash-escaped or as
// file:// URIs), and returns them.
func droppedPaths(s string) ([]string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	var raw []string
	if strings.HasPrefix(s, "file://") {
		raw = uriPaths(strings.ReplaceAll(s, " ", "\n"))
	} else {
		raw = splitShellWords(s)
	}
	if len(raw) == 0 {
		return nil, false
	}
	for i, p := range raw {
		if strings.HasPrefix(p, "~/") {
			p = filepath.Join(claude.Home(), p[2:])
		}
		if !filepath.IsAbs(p) {
			return nil, false
		}
		if _, err := os.Stat(p); err != nil {
			return nil, false
		}
		raw[i] = p
	}
	return raw, true
}

// splitShellWords splits on unquoted, unescaped spaces, as terminals quote
// dropped paths: '/a b/c.png' or /a\ b/c.png.
func splitShellWords(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	esc, has := false, false
	for _, r := range s {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\' && quote != '\'':
			esc, has = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, has = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if has || cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
		}
	}
	if has || cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
