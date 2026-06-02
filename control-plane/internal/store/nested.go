package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrNestedNotFound is returned when a tenant-scoped nested-entity op misses.
var ErrNestedNotFound = errors.New("nested entity not found for this tenant")

// ---- Ring group members ----

func (s *Store) GetRingGroupForTenant(ctx context.Context, tid, id uuid.UUID) (*RingGroup, error) {
	const q = `
		SELECT id, tenant_id, COALESCE(extension,''), name, strategy, ring_timeout_sec,
		       COALESCE(fallback_kind,''), fallback_id, COALESCE(caller_id_prefix,''),
		       enabled, created_at, updated_at
		  FROM ring_groups WHERE id = $1 AND tenant_id = $2`
	var rg RingGroup
	err := s.DB.QueryRow(ctx, q, id, tid).Scan(
		&rg.ID, &rg.TenantID, &rg.Extension, &rg.Name, &rg.Strategy, &rg.RingTimeoutSec,
		&rg.FallbackKind, &rg.FallbackID, &rg.CallerIDPrefix, &rg.Enabled, &rg.CreatedAt, &rg.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &rg, nil
}

// ListRingGroupMembersDetailed returns every member (including disabled) with
// the owning extension's number + display name, ordered by priority.
func (s *Store) ListRingGroupMembersDetailed(ctx context.Context, rgID uuid.UUID) ([]RingGroupMember, error) {
	const q = `
		SELECT rgm.id, rgm.ring_group_id, rgm.extension_id, rgm.priority,
		       rgm.ring_delay_sec, rgm.enabled,
		       e.extension, COALESCE(e.display_name,'')
		  FROM ring_group_members rgm
		  JOIN extensions e ON e.id = rgm.extension_id
		 WHERE rgm.ring_group_id = $1
		 ORDER BY rgm.priority, e.extension`
	rows, err := s.DB.Query(ctx, q, rgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RingGroupMember
	for rows.Next() {
		var m RingGroupMember
		if err := rows.Scan(
			&m.ID, &m.RingGroupID, &m.ExtensionID, &m.Priority,
			&m.RingDelaySec, &m.Enabled, &m.Extension, &m.DisplayName,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) DeleteRingGroupMemberForTenant(ctx context.Context, tid, memberID uuid.UUID) error {
	tag, err := s.DB.Exec(ctx, `
		DELETE FROM ring_group_members rgm
		 USING ring_groups rg
		 WHERE rgm.id = $1 AND rgm.ring_group_id = rg.id AND rg.tenant_id = $2`,
		memberID, tid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNestedNotFound
	}
	return nil
}

// ---- IVR options ----

func (s *Store) GetIVRForTenant(ctx context.Context, tid, id uuid.UUID) (*IVR, error) {
	const q = `
		SELECT id, tenant_id, name, COALESCE(extension,''),
		       greeting_long, greeting_short, invalid_sound, exit_sound,
		       timeout_ms, inter_digit_timeout_ms,
		       max_failures, max_timeouts, digit_len,
		       enabled, created_at, updated_at
		  FROM ivrs WHERE id = $1 AND tenant_id = $2`
	var v IVR
	err := s.DB.QueryRow(ctx, q, id, tid).Scan(
		&v.ID, &v.TenantID, &v.Name, &v.Extension,
		&v.GreetingLong, &v.GreetingShort, &v.InvalidSound, &v.ExitSound,
		&v.TimeoutMS, &v.InterDigitTimeoutMS,
		&v.MaxFailures, &v.MaxTimeouts, &v.DigitLen,
		&v.Enabled, &v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *Store) ListIVROptions(ctx context.Context, ivrID uuid.UUID) ([]IVROption, error) {
	const q = `
		SELECT id, ivr_id, digit, COALESCE(label,''), action_kind, action_id, COALESCE(action_data,'')
		  FROM ivr_options WHERE ivr_id = $1 ORDER BY digit`
	rows, err := s.DB.Query(ctx, q, ivrID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IVROption
	for rows.Next() {
		var o IVROption
		if err := rows.Scan(&o.ID, &o.IVRID, &o.Digit, &o.Label, &o.ActionKind, &o.ActionID, &o.ActionData); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) DeleteIVROptionForTenant(ctx context.Context, tid, optID uuid.UUID) error {
	tag, err := s.DB.Exec(ctx, `
		DELETE FROM ivr_options o
		 USING ivrs v
		 WHERE o.id = $1 AND o.ivr_id = v.id AND v.tenant_id = $2`,
		optID, tid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNestedNotFound
	}
	return nil
}

// ---- Queue agents ----

func (s *Store) GetQueueForTenant(ctx context.Context, tid, id uuid.UUID) (*Queue, error) {
	const q = `
		SELECT id, tenant_id, COALESCE(extension,''), name, strategy, moh_sound,
		       COALESCE(record_template,''), time_base_score,
		       max_wait_time, max_wait_no_agent, max_wait_no_agent_time_reached,
		       tier_rules_apply, tier_rule_wait_second, tier_rule_no_agent_no_wait,
		       discard_abandoned_after, abandoned_resume_allowed,
		       COALESCE(announce_sound,''),
		       enabled, created_at, updated_at
		  FROM queues WHERE id = $1 AND tenant_id = $2`
	var q2 Queue
	err := s.DB.QueryRow(ctx, q, id, tid).Scan(
		&q2.ID, &q2.TenantID, &q2.Extension, &q2.Name, &q2.Strategy, &q2.MOHSound,
		&q2.RecordTemplate, &q2.TimeBaseScore,
		&q2.MaxWaitTime, &q2.MaxWaitNoAgent, &q2.MaxWaitNoAgentTimeReached,
		&q2.TierRulesApply, &q2.TierRuleWaitSecond, &q2.TierRuleNoAgentNoWait,
		&q2.DiscardAbandonedAfter, &q2.AbandonedResumeAllowed,
		&q2.AnnounceSound,
		&q2.Enabled, &q2.CreatedAt, &q2.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &q2, nil
}

func (s *Store) ListQueueAgentsDetailed(ctx context.Context, queueID uuid.UUID) ([]QueueAgent, error) {
	const q = `
		SELECT qa.id, qa.queue_id, qa.extension_id, qa.agent_type, qa.tier_level, qa.tier_position,
		       qa.max_no_answer, qa.wrap_up_time, qa.reject_delay_time, qa.busy_delay_time,
		       qa.no_answer_delay_time, qa.enabled,
		       e.extension, COALESCE(e.display_name,'')
		  FROM queue_agents qa
		  JOIN extensions e ON e.id = qa.extension_id
		 WHERE qa.queue_id = $1
		 ORDER BY qa.tier_level, qa.tier_position, e.extension`
	rows, err := s.DB.Query(ctx, q, queueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueueAgent
	for rows.Next() {
		var a QueueAgent
		if err := rows.Scan(
			&a.ID, &a.QueueID, &a.ExtensionID, &a.AgentType, &a.TierLevel, &a.TierPosition,
			&a.MaxNoAnswer, &a.WrapUpTime, &a.RejectDelayTime, &a.BusyDelayTime,
			&a.NoAnswerDelayTime, &a.Enabled, &a.Extension, &a.DisplayName,
		); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteQueueAgentForTenant(ctx context.Context, tid, agentID uuid.UUID) error {
	tag, err := s.DB.Exec(ctx, `
		DELETE FROM queue_agents qa
		 USING queues q
		 WHERE qa.id = $1 AND qa.queue_id = q.id AND q.tenant_id = $2`,
		agentID, tid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNestedNotFound
	}
	return nil
}
