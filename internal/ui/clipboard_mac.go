package ui

// macOS clipboard: wl-paste/xclip don't exist there and pbpaste only gives
// text, so images and Finder-copied files are read through osascript's
// JavaScript bridge to NSPasteboard (built in, nothing to install). The file
// has no build tag so its logic is tested on Linux too, with osascript stubbed.

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

var goos = runtime.GOOS

// runJXA runs a JavaScript for Automation script and returns its output.
var runJXA = func(script string) (string, error) {
	out, err := exec.Command("osascript", "-l", "JavaScript", "-e", script).Output()
	return strings.TrimSpace(string(out)), err
}

const (
	jxaTypes = `ObjC.import('AppKit');
JSON.stringify(ObjC.deepUnwrap($.NSPasteboard.generalPasteboard.types) || [])`

	// PNG as base64; an image only offered as TIFF (copied from some apps) is
	// converted. Empty output: no image.
	jxaPNG = `ObjC.import('AppKit');
var pb = $.NSPasteboard.generalPasteboard;
var d = pb.dataForType('public.png');
if (d.isNil()) {
  var t = pb.dataForType('public.tiff');
  if (!t.isNil()) {
    var rep = $.NSBitmapImageRep.imageRepWithData(t);
    if (!rep.isNil()) d = rep.representationUsingTypeProperties(4, $()); // 4: PNG
  }
}
d.isNil() ? '' : d.base64EncodedStringWithOptions(0).js`

	// The file URLs on the clipboard (Finder's cmd+c), one per line.
	jxaFiles = `ObjC.import('AppKit');
var urls = $.NSPasteboard.generalPasteboard.readObjectsForClassesOptions($.NSArray.arrayWithObject($.NSURL), $());
var out = [];
if (!urls.isNil()) for (var i = 0; i < urls.count; i++) {
  var u = urls.objectAtIndex(i);
  if (u.isFileURL) out.push(u.absoluteString.js);
}
out.join('\n')`
)

// macClipboardTypes maps the pasteboard's type identifiers to the MIME types
// the rest of the clipboard code asks for.
func macClipboardTypes() []string {
	out, err := runJXA(jxaTypes)
	if err != nil {
		return nil
	}
	var utis []string
	if json.Unmarshal([]byte(out), &utis) != nil {
		return nil
	}
	var types []string
	add := func(t string) {
		for _, x := range types {
			if x == t {
				return
			}
		}
		types = append(types, t)
	}
	for _, u := range utis {
		switch u {
		case "public.png":
			add("image/png")
		case "public.tiff", "public.jpeg", "com.compuserve.gif":
			add("image/tiff") // read back as png (see macReadClipboardAs)
		case "public.file-url", "NSFilenamesPboardType":
			add("text/uri-list")
		case "public.utf8-plain-text", "NSStringPboardType":
			add("text/plain")
		}
	}
	return types
}

func macReadClipboardAs(mime string) ([]byte, error) {
	switch {
	case strings.HasPrefix(mime, "image/"):
		out, err := runJXA(jxaPNG)
		if err != nil || out == "" {
			return nil, err
		}
		return base64.StdEncoding.DecodeString(out)
	case mime == "text/uri-list":
		out, err := runJXA(jxaFiles)
		return []byte(out), err
	}
	return nil, os.ErrNotExist
}
