package atlassianserver

import (
	"testing"

	"github.com/icholy/xagent/internal/eventrouter2"
)

func TestSchemaRegistration(t *testing.T) {
	def, ok := eventrouter2.EventTypeFor("atlassian", EventTypeCommentCreated)
	if !ok {
		t.Fatalf("EventTypeFor(atlassian, %q) = _, false; want hit", EventTypeCommentCreated)
	}
	wantAttrs := []string{"body", "url", "mention"}
	if got := def.Attrs; len(got) != len(wantAttrs) {
		t.Errorf("comment_created Attrs = %v, want %v", got, wantAttrs)
	}
	if len(def.DefaultRules) == 0 {
		t.Errorf("comment_created DefaultRules is empty; want the xagent: wakeup rule")
	}

	// label_added registers but ships no default rules.
	labelDef, ok := eventrouter2.EventTypeFor("atlassian", EventTypeLabelAdded)
	if !ok {
		t.Fatalf("EventTypeFor(atlassian, %q) = _, false; want hit", EventTypeLabelAdded)
	}
	if labelDef.DefaultRules != nil {
		t.Errorf("label_added DefaultRules = %v, want nil", labelDef.DefaultRules)
	}
}
