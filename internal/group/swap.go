package group

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/layout"
	"github.com/farzeni/regia/internal/plugins"
)

// A session swap hands a task over to a fresh session: B takes A's name,
// place, pin and children (layout.SwapIn), and A is removed, so a task or
// epic keeps one row and one name while the session behind it changes (a
// stale coordinator replaced instead of woken with a cold cache). The row
// keeps the chain of swaps with a note, e.g. the handoff file.
//
// B is started by the caller beforehand. A must be done, idle or failed,
// unless A swaps itself out: then it is removed once its turn ends (Retire,
// run by the plugin's Stop hook, or by regia when it sees A idle). Plugins
// with "gates": ["swap"] may refuse. When claude rm refuses A (unpushed
// work), the swap stands and A stays archived under B with the reason.

// SwapIn is the swap of a for b, asked by the agent self through regia mcp:
// scoped like remove, except that a may be the caller itself.
func SwapIn(self, a, b, reason, note string) (string, error) {
	if !Enabled() {
		return "", ErrOff
	}
	if self == "" {
		return "", errors.New("regia can't tell which agent you are (no Claude Code session in the environment)")
	}
	return swap(self, a, b, reason, note)
}

// SwapAsYou is the swap from the command line (regia swap): it runs as you,
// so nothing is out of scope.
func SwapAsYou(a, b, reason, note string) (string, error) {
	return swap("", a, b, reason, note)
}

// SwapFor is SwapAsYou on your behalf, named in U by who: a plugin's action
// you picked, or one your attach gate ran.
func SwapFor(who, a, b, reason, note string) (string, error) {
	return swapNamed("", who, a, b, reason, note)
}

func swap(self, a, b, reason, note string) (string, error) {
	return swapNamed(self, "", a, b, reason, note)
}

func swapNamed(self, who, a, b, reason, note string) (string, error) {
	reason, note = strings.TrimSpace(reason), strings.TrimSpace(note)
	if reason == "" {
		return "", errors.New("give the reason (the user sees it in regia, under U)")
	}
	agents := Agents()
	w := newWorld(layout.Load(), agents)
	by, name := "you", "you (regia swap)"
	if who != "" {
		name = who
	}
	if self != "" {
		c := w.who(self)
		by, name = self, c.Name
		if a != self {
			if why := c.canSession(w, a); why != "" {
				return "", errors.New(why)
			}
		}
		if b != self {
			if why := c.canSession(w, b); why != "" {
				return "", errors.New(why)
			}
		}
	}
	for _, id := range []string{a, b} {
		ag := w.agents[id]
		switch {
		case ag == nil:
			return "", fmt.Errorf("%s is not a listed agent (ids are in tree)", id)
		case ag.Interactive:
			return "", fmt.Errorf("%s (%s) is an interactive session: only background sessions can be swapped", w.title(id), id)
		}
	}
	if a == b {
		return "", errors.New("a session can't replace itself")
	}
	own := a == self // swapping itself out, in its own turn
	if !own {
		claude.Enrich(w.agents[a])
		switch w.agents[a].State {
		case "busy":
			return "", fmt.Errorf("%s is working: wait until it is done, or stop it first", w.title(a))
		case "blocked":
			return "", fmt.Errorf("%s is waiting on the user", w.title(a))
		}
	}

	in := w.gateInput(a)
	to := PluginAgent(w.agents[b])
	to.Title = w.title(b)
	in.To, in.Note, in.By, in.Reason = &to, note, name, reason
	if err := plugins.Gate(plugins.GateSwap, in, in.Agent.Cwd); err != nil {
		return "", err
	}

	titleA, titleB := w.title(a), w.title(b)
	sw := layout.Swap{At: time.Now(), By: by, Reason: reason, Note: note}
	_, ch, err := layout.Update(func(l *layout.Layout) error {
		newWorld(l, agents)
		if err := l.SwapIn(a, b, titleA, sw, agents); err != nil {
			return err
		}
		if own {
			if l.Retire == nil {
				l.Retire = map[string]string{}
			}
			l.Retire[a] = reason
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	summary := fmt.Sprintf("swapped %s (%s) for %s (%s): %s", titleA, a, titleB, b, reason)
	if note != "" {
		summary += " · " + note
	}
	_ = Record(Action{At: time.Now(), By: by, Name: name, Tool: "swap", Summary: summary, Change: ch, Final: true})

	if own {
		return summary + "; you are removed once this turn ends", nil
	}
	if out, err := RunRm(a); err != nil {
		why := oneLine(out, err)
		_, _, _ = layout.Update(func(l *layout.Layout) error { l.NoteRmErr(b, a, why); return nil })
		return summary + "; claude rm refused " + titleA + " (" + why + "): it stays archived under " + titleB, nil
	}
	return summary, nil
}

// --- removal after its own turn ---------------------------------------------

// HandleStop is the plugin's Stop hook (regia hook stop): when this session
// swapped itself out, regia retire removes it in the background once the turn
// is over. Quiet and fast (one file read when there is nothing to do), it
// never fails the hook.
func HandleStop(stdin io.Reader, jobDir string) {
	var in HookInput
	_ = json.NewDecoder(io.LimitReader(stdin, 1<<20)).Decode(&in)
	self := SelfID(jobDir, in.SessionID)
	if self == "" || layout.Load().Retire[self] == "" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	c := exec.Command(exe, "retire", self)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // outlives the session it removes
	if c.Start() == nil {
		_ = c.Process.Release()
	}
}

// retireWait is how long Retire waits for the session's turn to end.
var retireWait = 2 * time.Minute

// Retire removes a session that swapped itself out, once it is no longer
// working (regia retire, started by the Stop hook; regia's TUI does the same
// when it sees the session idle). Run twice, the second finds nothing to do.
func Retire(id string) error {
	for deadline := time.Now().Add(retireWait); ; time.Sleep(2 * time.Second) {
		if layout.Load().Retire[id] == "" {
			return nil // done already
		}
		a := Agents()[id]
		if a == nil {
			break // gone already
		}
		claude.Enrich(a)
		if a.State != "busy" {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s is still working: left for later", id)
		}
	}
	var rmErr string
	if Agents()[id] != nil {
		if out, err := RunRm(id); err != nil && Agents()[id] != nil { // gone anyway: removed by the other path
			rmErr = oneLine(out, err)
		}
	}
	_, _, err := layout.Update(func(l *layout.Layout) error {
		delete(l.Retire, id)
		if b, _ := l.Replaced(id); b != "" && rmErr != "" {
			l.NoteRmErr(b, id, rmErr)
		}
		return nil
	})
	if rmErr != "" {
		return errors.New(rmErr)
	}
	return err
}
