package freeswitch

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// buildRingGroupDialString constructs an FS bridge dialstring for a ring
// group. Each member becomes one leg; separator is comma (simultaneous) or
// pipe (sequential). For sequential, each leg gets its own [leg_timeout=N]
// so FS rings that member for ringTimeout then moves on.
//
// kamailioSIPTarget is host:port — every leg routes back through Kamailio
// so the AOR lookup happens in the registrar, not in FS.
//
// Returns empty string if no enabled members.
func buildRingGroupDialString(members []store.RingGroupMember, strategy string, ringTimeout int, kamailioSIPTarget string) string {
	if len(members) == 0 {
		return ""
	}
	parts := make([]string, 0, len(members))
	for _, m := range members {
		if !m.Enabled || m.SIPUsername == "" || m.SIPDomain == "" {
			continue
		}
		leg := fmt.Sprintf("sofia/internal/sip:%s@%s;fs_path=sip:%s;lr",
			m.SIPUsername, m.SIPDomain, kamailioSIPTarget)

		// Sequential: hand-allotted leg timeout, so FS doesn't camp on one
		// member forever before moving to the next.
		if strategy == "sequential" {
			leg = fmt.Sprintf("[leg_timeout=%d]%s", ringTimeout, leg)
		}
		parts = append(parts, leg)
	}
	if len(parts) == 0 {
		return ""
	}
	sep := ","
	if strategy == "sequential" {
		sep = "|"
	}
	return strings.Join(parts, sep)
}

// supportedRingGroupStrategy reports whether the dialplan handler knows how
// to render this strategy. All four strategies are now wired (Wave 1.5).
func supportedRingGroupStrategy(strategy string) bool {
	switch strategy {
	case "simultaneous", "sequential", "round_robin", "random":
		return true
	default:
		return false
	}
}

// rotateMembers returns members rotated so that members[start] is first.
// Used by round-robin: each call picks a different starting member, then
// falls through to the rest sequentially if the chosen agent doesn't answer.
//
// start is taken mod len(members) to be defensive against off-by-one bugs.
func rotateMembers(members []store.RingGroupMember, start int) []store.RingGroupMember {
	n := len(members)
	if n == 0 {
		return members
	}
	start = ((start % n) + n) % n // handle negative defensively
	if start == 0 {
		return members
	}
	out := make([]store.RingGroupMember, 0, n)
	out = append(out, members[start:]...)
	out = append(out, members[:start]...)
	return out
}

// shuffleMembers returns a fresh-shuffled copy. Uses math/rand/v2 which is
// auto-seeded and not goroutine-locked (PCG-based).
func shuffleMembers(members []store.RingGroupMember) []store.RingGroupMember {
	out := make([]store.RingGroupMember, len(members))
	copy(out, members)
	rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}
