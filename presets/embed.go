// Package presets holds the opener presets that ship with regia: small
// `key = value` files in openers/, picked by name with `preset = <name>` in
// ~/.config/regia/openers.conf. Write one for your editor and send it back.
package presets

import "embed"

// Openers is presets/openers/*.conf, built into the binary.
//
//go:embed openers/*.conf
var Openers embed.FS
