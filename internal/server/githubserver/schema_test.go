package githubserver

import (
	"testing"

	"github.com/icholy/xagent/internal/eventrouter2"
)

func TestSchemaRegistration(t *testing.T) {
	def, ok := eventrouter2.EventTypeFor("github", EventTypeIssueComment)
	if !ok {
		t.Fatalf("EventTypeFor(github, %q) = _, false; want hit", EventTypeIssueComment)
	}
	wantAttrs := []string{"body", "url", "mention"}
	if got := def.Attrs; len(got) != len(wantAttrs) {
		t.Errorf("issue_comment Attrs = %v, want %v", got, wantAttrs)
	}
	if len(def.DefaultRules) == 0 {
		t.Errorf("issue_comment DefaultRules is empty; want the xagent: wakeup rule")
	}

	// A non-comment type registers but ships no default rules.
	labelDef, ok := eventrouter2.EventTypeFor("github", EventTypeLabelAdded)
	if !ok {
		t.Fatalf("EventTypeFor(github, %q) = _, false; want hit", EventTypeLabelAdded)
	}
	if labelDef.DefaultRules != nil {
		t.Errorf("label_added DefaultRules = %v, want nil", labelDef.DefaultRules)
	}
}
