package group

import (
	"errors"
	"fmt"
	"strings"
)

// Claim lets the caller organize its group ("group") or the whole tree
// ("tree"), saying why. regia shows the claim (✦ and the reason) and ends it
// when the agent is done; Release ends it sooner.
func Claim(self, scope, reason string) (string, error) {
	reason = strings.TrimSpace(strings.Join(strings.Fields(reason), " "))
	if reason == "" {
		return "", errors.New("say why you claim it (the user sees the reason in regia)")
	}
	return change(self, "claim", func(w *world, c Caller) (string, error) {
		if w.l.Grants[self] == Everything && w.l.Claims[self] == "" {
			return "", errors.New("the user already granted you the whole tree: no claim needed")
		}
		var to string
		switch scope {
		case "tree":
			to = Everything
		case "group":
			if c.Group == "" {
				return "", errors.New("you are in no group: claim the tree instead")
			}
			to = c.Group
		default:
			return "", fmt.Errorf("claim %q: say group or tree", scope)
		}
		w.l.Grants[self], w.l.Claims[self] = to, reason
		if to == Everything {
			return "claimed the whole tree: " + reason, nil
		}
		return fmt.Sprintf("claimed %q: %s", w.title(to), reason), nil
	})
}

// Release ends the caller's claim.
func Release(self string) (string, error) {
	return change(self, "release", func(w *world, c Caller) (string, error) {
		if w.l.Claims[self] == "" {
			if w.l.Grants[self] != "" {
				return "", errors.New("the user granted you this in regia: only they can take it back (O)")
			}
			return "", errors.New("you have no claim to release")
		}
		delete(w.l.Grants, self)
		delete(w.l.Claims, self)
		return "released its claim", nil
	})
}
