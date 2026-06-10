package freeswitch

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// flattenCallback joins actions into "app=data\n" lines for substring asserts.
func flattenCallback(actions []dialplanAction) string {
	var sb strings.Builder
	for _, a := range actions {
		sb.WriteString(a.App + "=" + a.Data + "\n")
	}
	return sb.String()
}

func TestBuildCallbackOfferActions(t *testing.T) {
	tid := uuid.New()
	q := &store.Queue{ID: uuid.New(), TenantID: tid, Extension: "700", Name: "Support"}

	flat := flattenCallback(buildCallbackOfferActions(q))

	for _, want := range []string{
		"answer=",
		"x_tenant_id=" + tid.String(),
		"x_queue_id=" + q.ID.String(),
		"x_queue_callback_offer=true",
		"play_and_get_digits=",
		"pagd_input",           // captures the opt-in digit
		"ivr-callback_offer",   // the (placeholder) offer prompt
		"ivr-callback_confirm", // the (placeholder) confirmation prompt
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("callback offer actions missing %q\n--- got ---\n%s", want, flat)
		}
	}

	// Non-opt-in path transfers back into the queue's own extension.
	if !strings.Contains(flat, "transfer=") || !strings.Contains(flat, "700 XML default") {
		t.Errorf("callback offer should transfer non-opt-in callers into queue 700\n--- got ---\n%s", flat)
	}
}

func TestBuildCallbackOfferActionsNoQueueExtension(t *testing.T) {
	tid := uuid.New()
	// A queue reached via a DID has no internal extension; the offer should still
	// answer + record but have nowhere to transfer back to (no transfer action).
	q := &store.Queue{ID: uuid.New(), TenantID: tid, Name: "Inbound"}

	actions := buildCallbackOfferActions(q)
	flat := flattenCallback(actions)

	if !strings.Contains(flat, "play_and_get_digits=") {
		t.Errorf("offer should still prompt without a queue extension\n%s", flat)
	}
	for _, a := range actions {
		if a.App == "transfer" {
			t.Errorf("no queue extension → no transfer action, got %q", a.Data)
		}
	}
}
