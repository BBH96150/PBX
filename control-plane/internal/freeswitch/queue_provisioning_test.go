package freeswitch

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func TestBuildQueueProvisionCommands_EmptyOrNil(t *testing.T) {
	if cmds := BuildQueueProvisionCommands(nil); cmds != nil {
		t.Errorf("nil config: expected nil, got %v", cmds)
	}
	if cmds := BuildQueueProvisionCommands(&store.CallcenterConfig{}); cmds != nil {
		t.Errorf("empty config: expected nil, got %v", cmds)
	}
}

func TestBuildQueueProvisionCommands_FullStack(t *testing.T) {
	qID := uuid.New()
	ext1ID := uuid.New()
	ext2ID := uuid.New()
	cfg := &store.CallcenterConfig{
		Queues: []store.CallcenterQueue{
			{
				Name: qID.String(),
				Queue: store.Queue{
					ID:       qID,
					Strategy: "longest-idle-agent",
				},
			},
		},
		Agents: []store.CallcenterAgent{
			{
				Name:              "agent_" + ext1ID.String(),
				Type:              "callback",
				Contact:           "[call_timeout=20]sofia/internal/sip:101@acme.sip.local;fs_path=sip:kamailio:5060;lr",
				MaxNoAnswer:       3,
				WrapUpTime:        10,
				RejectDelayTime:   10,
				BusyDelayTime:     60,
				NoAnswerDelayTime: 30,
			},
			{
				Name:              "agent_" + ext2ID.String(),
				Type:              "callback",
				Contact:           "[call_timeout=20]sofia/internal/sip:102@acme.sip.local;fs_path=sip:kamailio:5060;lr",
				MaxNoAnswer:       5,
				WrapUpTime:        30,
				RejectDelayTime:   10,
				BusyDelayTime:     60,
				NoAnswerDelayTime: 30,
			},
		},
		Tiers: []store.CallcenterTier{
			{Agent: "agent_" + ext1ID.String(), Queue: qID.String(), Level: 1, Position: 1},
			{Agent: "agent_" + ext2ID.String(), Queue: qID.String(), Level: 2, Position: 1},
		},
	}

	cmds := BuildQueueProvisionCommands(cfg)
	joined := strings.Join(cmds, "\n")

	wants := []string{
		"callcenter_config queue load " + qID.String(),
		"callcenter_config agent add agent_" + ext1ID.String() + " callback",
		"callcenter_config agent set contact agent_" + ext1ID.String() + " '[call_timeout=20]sofia/internal/sip:101@acme.sip.local;fs_path=sip:kamailio:5060;lr'",
		"callcenter_config agent set status agent_" + ext1ID.String() + " Available",
		"callcenter_config agent set wrap_up_time agent_" + ext2ID.String() + " 30",
		"callcenter_config tier add " + qID.String() + " agent_" + ext1ID.String() + " 1 1",
		"callcenter_config tier add " + qID.String() + " agent_" + ext2ID.String() + " 2 1",
	}
	for _, w := range wants {
		if !strings.Contains(joined, w) {
			t.Errorf("missing command line %q\nfull output:\n%s", w, joined)
		}
	}

	// Sanity check ordering — queue load before agent ops, agent ops before tier add.
	queueLoadIdx := strings.Index(joined, "queue load")
	firstAgentIdx := strings.Index(joined, "agent add")
	firstTierIdx := strings.Index(joined, "tier add")
	if !(queueLoadIdx < firstAgentIdx && firstAgentIdx < firstTierIdx) {
		t.Errorf("command order wrong: queue=%d agent=%d tier=%d",
			queueLoadIdx, firstAgentIdx, firstTierIdx)
	}
}
