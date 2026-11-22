package group

import (
	"strings"
	"testing"

	"github.com/farzeni/regia/internal/layout"
)

func TestClaimAndRelease(t *testing.T) {
	toolsFixture(t)
	// a worker claims its group (coord's agents) and may then organize it
	_, err := Claim("w1", "group", "  ")
	refused(t, err, "say why")
	s, err := Claim("w1", "group", "split  the KPI work")
	if err != nil || s != `claimed "orchestrator": split the KPI work` {
		t.Fatalf("claim: %q %v", s, err)
	}
	me, _ := Whoami("w1")
	if me.Role != RoleOrchestrator || me.Scope != "coord" || me.Claim != "split the KPI work" || !strings.Contains(me.Can, "release") {
		t.Fatalf("after the claim: %+v", me)
	}
	if _, err := RenameAgent("w1", "w2", "layout v2"); err != nil {
		t.Fatalf("organize the claimed group: %v", err)
	}
	_, err = Move("w1", []string{"a1"}, "coord", "")
	refused(t, err, "claim the whole tree first")

	// the whole tree, but a claim never reaches muted parts
	if _, err := Claim("w1", "tree", "morning triage"); err != nil {
		t.Fatal(err)
	}
	if _, err := Move("w1", []string{"lonely"}, "g:1", ""); err != nil {
		t.Fatalf("whole tree: %v", err)
	}
	layout.Update(func(l *layout.Layout) error { l.Muted["p:x"] = true; return nil })
	_, err = Archive("w1", []string{"pp"}, true)
	refused(t, err, "muted")

	if _, err := Release("w1"); err != nil {
		t.Fatal(err)
	}
	if l := layout.Load(); l.Grants["w1"] != "" || l.Claims["w1"] != "" {
		t.Fatalf("released: %v %v", l.Grants, l.Claims)
	}
	_, err = Release("w1")
	refused(t, err, "no claim")

	// the user's grants are theirs to take back
	grant(t, "boss", Everything)
	_, err = Claim("boss", "tree", "x")
	refused(t, err, "already granted")
	_, err = Release("boss")
	refused(t, err, "only they can take it back")
	_, err = Claim("boss", "group", "x")
	refused(t, err, "already granted")

	var tools []string
	for _, a := range Actions() {
		tools = append(tools, a.Tool)
	}
	if strings.Join(tools, ",") != "claim,rename_agent,claim,move,release" {
		t.Fatalf("claims are logged: %v", tools)
	}
}

func TestClaimOutsideAnyGroup(t *testing.T) {
	toolsFixture(t)
	_, err := Claim("boss", "group", "x")
	refused(t, err, "in no group")
	_, err = Claim("boss", "everything", "x")
	refused(t, err, "say group or tree")
}
