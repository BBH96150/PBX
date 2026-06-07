package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Queue struct {
	ID                        uuid.UUID `json:"id"`
	TenantID                  uuid.UUID `json:"tenant_id"`
	Extension                 string    `json:"extension,omitempty"`
	Name                      string    `json:"name"`
	Strategy                  string    `json:"strategy"`
	MOHSound                  string    `json:"moh_sound"`
	RecordTemplate            string    `json:"record_template,omitempty"`
	TimeBaseScore             string    `json:"time_base_score"`
	MaxWaitTime               int       `json:"max_wait_time"`
	MaxWaitNoAgent            int       `json:"max_wait_no_agent"`
	MaxWaitNoAgentTimeReached int       `json:"max_wait_no_agent_time_reached"`
	TierRulesApply            bool      `json:"tier_rules_apply"`
	TierRuleWaitSecond        int       `json:"tier_rule_wait_second"`
	TierRuleNoAgentNoWait     bool      `json:"tier_rule_no_agent_no_wait"`
	DiscardAbandonedAfter     int       `json:"discard_abandoned_after"`
	AbandonedResumeAllowed    bool      `json:"abandoned_resume_allowed"`
	AnnounceSound             string    `json:"announce_sound,omitempty"`
	Enabled                   bool      `json:"enabled"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

type QueueAgent struct {
	ID                uuid.UUID `json:"id"`
	QueueID           uuid.UUID `json:"queue_id"`
	ExtensionID       uuid.UUID `json:"extension_id"`
	AgentType         string    `json:"agent_type"`
	TierLevel         int       `json:"tier_level"`
	TierPosition      int       `json:"tier_position"`
	MaxNoAnswer       int       `json:"max_no_answer"`
	WrapUpTime        int       `json:"wrap_up_time"`
	RejectDelayTime   int       `json:"reject_delay_time"`
	BusyDelayTime     int       `json:"busy_delay_time"`
	NoAnswerDelayTime int       `json:"no_answer_delay_time"`
	Enabled           bool      `json:"enabled"`
	// Joined fields populated by detail queries:
	Extension   string `json:"extension,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

type CreateQueueInput struct {
	TenantID    uuid.UUID
	Extension   string
	Name        string
	Strategy    string // default longest-idle-agent
	MOHSound    string // default local_stream://moh
	MaxWaitTime int
}

func (s *Store) CreateQueue(ctx context.Context, in CreateQueueInput) (*Queue, error) {
	const q = `
		INSERT INTO queues (tenant_id, extension, name, strategy, moh_sound, max_wait_time)
		VALUES ($1, NULLIF($2,''), $3,
		        COALESCE(NULLIF($4,''), 'longest-idle-agent'),
		        COALESCE(NULLIF($5,''), 'local_stream://moh'),
		        $6)
		RETURNING id, tenant_id, COALESCE(extension,''), name, strategy, moh_sound,
		          COALESCE(record_template,''), time_base_score,
		          max_wait_time, max_wait_no_agent, max_wait_no_agent_time_reached,
		          tier_rules_apply, tier_rule_wait_second, tier_rule_no_agent_no_wait,
		          discard_abandoned_after, abandoned_resume_allowed,
		          COALESCE(announce_sound,''),
		          enabled, created_at, updated_at`
	var qq Queue
	err := s.DB.QueryRow(ctx, q,
		in.TenantID, in.Extension, in.Name, in.Strategy, in.MOHSound, in.MaxWaitTime,
	).Scan(
		&qq.ID, &qq.TenantID, &qq.Extension, &qq.Name, &qq.Strategy, &qq.MOHSound,
		&qq.RecordTemplate, &qq.TimeBaseScore,
		&qq.MaxWaitTime, &qq.MaxWaitNoAgent, &qq.MaxWaitNoAgentTimeReached,
		&qq.TierRulesApply, &qq.TierRuleWaitSecond, &qq.TierRuleNoAgentNoWait,
		&qq.DiscardAbandonedAfter, &qq.AbandonedResumeAllowed,
		&qq.AnnounceSound,
		&qq.Enabled, &qq.CreatedAt, &qq.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &qq, nil
}

type AddQueueAgentInput struct {
	QueueID      uuid.UUID
	ExtensionID  uuid.UUID
	TierLevel    int
	TierPosition int
	WrapUpTime   int
}

func (s *Store) AddQueueAgent(ctx context.Context, in AddQueueAgentInput) (*QueueAgent, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const checkQ = `
		SELECT q.tenant_id = e.tenant_id
		  FROM queues q, extensions e
		 WHERE q.id = $1 AND e.id = $2`
	var sameTenant bool
	if err := tx.QueryRow(ctx, checkQ, in.QueueID, in.ExtensionID).Scan(&sameTenant); err != nil {
		return nil, err
	}
	if !sameTenant {
		return nil, ErrCrossTenant
	}

	if in.TierLevel == 0 {
		in.TierLevel = 1
	}
	if in.TierPosition == 0 {
		in.TierPosition = 1
	}
	const ins = `
		INSERT INTO queue_agents
		    (queue_id, extension_id, tier_level, tier_position, wrap_up_time)
		VALUES ($1, $2, $3, $4, COALESCE(NULLIF($5,0), 10))
		RETURNING id, queue_id, extension_id, agent_type, tier_level, tier_position,
		          max_no_answer, wrap_up_time, reject_delay_time, busy_delay_time,
		          no_answer_delay_time, enabled`
	var a QueueAgent
	err = tx.QueryRow(ctx, ins,
		in.QueueID, in.ExtensionID, in.TierLevel, in.TierPosition, in.WrapUpTime,
	).Scan(
		&a.ID, &a.QueueID, &a.ExtensionID, &a.AgentType, &a.TierLevel, &a.TierPosition,
		&a.MaxNoAnswer, &a.WrapUpTime, &a.RejectDelayTime, &a.BusyDelayTime,
		&a.NoAnswerDelayTime, &a.Enabled,
	)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &a, nil
}

// LookupQueueByExtension resolves an internal-dialed number to a queue for
// the given tenant.
func (s *Store) LookupQueueByExtension(ctx context.Context, tenantDomain, ext string) (*Queue, error) {
	const q = `
		SELECT q.id, q.tenant_id, COALESCE(q.extension,''), q.name, q.strategy, q.moh_sound,
		       COALESCE(q.record_template,''), q.time_base_score,
		       q.max_wait_time, q.max_wait_no_agent, q.max_wait_no_agent_time_reached,
		       q.tier_rules_apply, q.tier_rule_wait_second, q.tier_rule_no_agent_no_wait,
		       q.discard_abandoned_after, q.abandoned_resume_allowed,
		       COALESCE(q.announce_sound,''),
		       q.enabled, q.created_at, q.updated_at
		  FROM queues q
		  JOIN sip_domains sd ON sd.tenant_id = q.tenant_id
		 WHERE sd.domain = $1 AND q.extension = $2 AND q.enabled = true
		 LIMIT 1`
	return scanOneQueue(s.DB.QueryRow(ctx, q, tenantDomain, ext))
}

// LookupDIDQueueTarget resolves an inbound DID whose destination_kind='queue'.
func (s *Store) LookupDIDQueueTarget(ctx context.Context, e164 string) (*Queue, error) {
	const q = `
		SELECT qq.id, qq.tenant_id, COALESCE(qq.extension,''), qq.name, qq.strategy, qq.moh_sound,
		       COALESCE(qq.record_template,''), qq.time_base_score,
		       qq.max_wait_time, qq.max_wait_no_agent, qq.max_wait_no_agent_time_reached,
		       qq.tier_rules_apply, qq.tier_rule_wait_second, qq.tier_rule_no_agent_no_wait,
		       qq.discard_abandoned_after, qq.abandoned_resume_allowed,
		       COALESCE(qq.announce_sound,''),
		       qq.enabled, qq.created_at, qq.updated_at
		  FROM dids d
		  JOIN queues qq ON qq.id = d.destination_id AND d.destination_kind = 'queue'
		 WHERE d.e164 = $1 AND d.enabled = true AND qq.enabled = true
		 LIMIT 1`
	return scanOneQueue(s.DB.QueryRow(ctx, q, e164))
}

type queueRow interface {
	Scan(dest ...any) error
}

func scanOneQueue(r queueRow) (*Queue, error) {
	var qq Queue
	err := r.Scan(
		&qq.ID, &qq.TenantID, &qq.Extension, &qq.Name, &qq.Strategy, &qq.MOHSound,
		&qq.RecordTemplate, &qq.TimeBaseScore,
		&qq.MaxWaitTime, &qq.MaxWaitNoAgent, &qq.MaxWaitNoAgentTimeReached,
		&qq.TierRulesApply, &qq.TierRuleWaitSecond, &qq.TierRuleNoAgentNoWait,
		&qq.DiscardAbandonedAfter, &qq.AbandonedResumeAllowed,
		&qq.AnnounceSound,
		&qq.Enabled, &qq.CreatedAt, &qq.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &qq, nil
}

// CallcenterConfig is what the FreeSWITCH configuration handler needs to
// render callcenter.conf — all enabled queues + their agents + the tier
// links between them. One round-trip per FS reload.
type CallcenterConfig struct {
	Queues []CallcenterQueue
	Agents []CallcenterAgent
	Tiers  []CallcenterTier
}

type CallcenterQueue struct {
	Name string // queue.id (string)
	Queue
}

type CallcenterAgent struct {
	Name              string // "agent_<extension_id>"
	Type              string
	Contact           string // sofia/internal/sip:<user>@<domain>;fs_path=...
	MaxNoAnswer       int
	WrapUpTime        int
	RejectDelayTime   int
	BusyDelayTime     int
	NoAnswerDelayTime int
}

type CallcenterTier struct {
	Agent    string // matches CallcenterAgent.Name
	Queue    string // matches CallcenterQueue.Name
	Level    int
	Position int
}

// ListCallcenterConfig returns everything needed to render callcenter.conf.
// kamailioTarget is host:port — used to build agent contact URIs that route
// back through Kamailio (so AOR lookup stays in the registrar).
func (s *Store) ListCallcenterConfig(ctx context.Context, kamailioTarget string) (*CallcenterConfig, error) {
	cfg := &CallcenterConfig{}

	const qQ = `
		SELECT id, tenant_id, COALESCE(extension,''), name, strategy, moh_sound,
		       COALESCE(record_template,''), time_base_score,
		       max_wait_time, max_wait_no_agent, max_wait_no_agent_time_reached,
		       tier_rules_apply, tier_rule_wait_second, tier_rule_no_agent_no_wait,
		       discard_abandoned_after, abandoned_resume_allowed,
		       COALESCE(announce_sound,''),
		       enabled, created_at, updated_at
		  FROM queues WHERE enabled = true ORDER BY created_at`
	rows, err := s.DB.Query(ctx, qQ)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var q Queue
		if err := rows.Scan(
			&q.ID, &q.TenantID, &q.Extension, &q.Name, &q.Strategy, &q.MOHSound,
			&q.RecordTemplate, &q.TimeBaseScore,
			&q.MaxWaitTime, &q.MaxWaitNoAgent, &q.MaxWaitNoAgentTimeReached,
			&q.TierRulesApply, &q.TierRuleWaitSecond, &q.TierRuleNoAgentNoWait,
			&q.DiscardAbandonedAfter, &q.AbandonedResumeAllowed,
			&q.AnnounceSound,
			&q.Enabled, &q.CreatedAt, &q.UpdatedAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		cfg.Queues = append(cfg.Queues, CallcenterQueue{Name: q.ID.String(), Queue: q})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	const aQ = `
		SELECT qa.queue_id, qa.extension_id, qa.agent_type,
		       qa.tier_level, qa.tier_position,
		       qa.max_no_answer, qa.wrap_up_time, qa.reject_delay_time,
		       qa.busy_delay_time, qa.no_answer_delay_time,
		       e.sip_username, sd.domain
		  FROM queue_agents qa
		  JOIN extensions  e  ON e.id  = qa.extension_id AND e.status = 'active'
		  JOIN sip_domains sd ON sd.id = e.sip_domain_id
		 WHERE qa.enabled = true
		 ORDER BY qa.queue_id, qa.tier_level, qa.tier_position`
	arows, err := s.DB.Query(ctx, aQ)
	if err != nil {
		return nil, err
	}
	defer arows.Close()

	// agent_<extension_id> may appear in multiple queues; mod_callcenter
	// wants one <agent> entry but many <tier> entries. Dedupe agents.
	seenAgents := map[string]bool{}
	for arows.Next() {
		var (
			queueID, extID                      uuid.UUID
			agentType                           string
			tierLevel, tierPos                  int
			maxNA, wrap, rejectD, busyD, noAnsD int
			sipUser, sipDomain                  string
		)
		if err := arows.Scan(
			&queueID, &extID, &agentType, &tierLevel, &tierPos,
			&maxNA, &wrap, &rejectD, &busyD, &noAnsD,
			&sipUser, &sipDomain,
		); err != nil {
			return nil, err
		}
		agentName := "agent_" + extID.String()
		queueName := queueID.String()

		if !seenAgents[agentName] {
			seenAgents[agentName] = true
			cfg.Agents = append(cfg.Agents, CallcenterAgent{
				Name:              agentName,
				Type:              agentType,
				Contact:           "[call_timeout=20]sofia/internal/sip:" + sipUser + "@" + sipDomain + ";fs_path=sip:" + kamailioTarget + ";lr",
				MaxNoAnswer:       maxNA,
				WrapUpTime:        wrap,
				RejectDelayTime:   rejectD,
				BusyDelayTime:     busyD,
				NoAnswerDelayTime: noAnsD,
			})
		}
		cfg.Tiers = append(cfg.Tiers, CallcenterTier{
			Agent:    agentName,
			Queue:    queueName,
			Level:    tierLevel,
			Position: tierPos,
		})
	}
	return cfg, arows.Err()
}

// LoadOneQueueConfig returns the CallcenterConfig narrowed to a single queue
// (with all of its agents + tiers). Used by Wave 4.5 live provisioning so
// only the affected queue is pushed to FS, not the entire pool.
func (s *Store) LoadOneQueueConfig(ctx context.Context, queueID uuid.UUID, kamailioTarget string) (*CallcenterConfig, error) {
	return loadFilteredCallcenterConfig(ctx, s, kamailioTarget,
		` AND q.id = $1 `,
		` AND qa.queue_id = $1 `,
		queueID)
}

// LoadQueueConfigsForExtension returns one CallcenterConfig per queue this
// extension agents into. Used when an admin adds an extension to a queue
// (or any agent-level field changes).
func (s *Store) LoadQueueConfigsForExtension(ctx context.Context, extID uuid.UUID, kamailioTarget string) ([]*CallcenterConfig, error) {
	const qIDs = `
		SELECT DISTINCT queue_id FROM queue_agents
		 WHERE extension_id = $1 AND enabled = true`
	rows, err := s.DB.Query(ctx, qIDs, extID)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]*CallcenterConfig, 0, len(ids))
	for _, id := range ids {
		cfg, err := s.LoadOneQueueConfig(ctx, id, kamailioTarget)
		if err != nil {
			return nil, err
		}
		out = append(out, cfg)
	}
	return out, nil
}

// loadFilteredCallcenterConfig is the parameterized core of
// ListCallcenterConfig — kept private to avoid copy-paste.
func loadFilteredCallcenterConfig(ctx context.Context, s *Store, kamailioTarget, queueWhere, agentWhere string, args ...any) (*CallcenterConfig, error) {
	cfg := &CallcenterConfig{}

	qQ := `
		SELECT id, tenant_id, COALESCE(extension,''), name, strategy, moh_sound,
		       COALESCE(record_template,''), time_base_score,
		       max_wait_time, max_wait_no_agent, max_wait_no_agent_time_reached,
		       tier_rules_apply, tier_rule_wait_second, tier_rule_no_agent_no_wait,
		       discard_abandoned_after, abandoned_resume_allowed,
		       COALESCE(announce_sound,''),
		       enabled, created_at, updated_at
		  FROM queues q
		 WHERE enabled = true ` + queueWhere + `
		 ORDER BY created_at`
	rows, err := s.DB.Query(ctx, qQ, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var q Queue
		if err := rows.Scan(
			&q.ID, &q.TenantID, &q.Extension, &q.Name, &q.Strategy, &q.MOHSound,
			&q.RecordTemplate, &q.TimeBaseScore,
			&q.MaxWaitTime, &q.MaxWaitNoAgent, &q.MaxWaitNoAgentTimeReached,
			&q.TierRulesApply, &q.TierRuleWaitSecond, &q.TierRuleNoAgentNoWait,
			&q.DiscardAbandonedAfter, &q.AbandonedResumeAllowed,
			&q.AnnounceSound,
			&q.Enabled, &q.CreatedAt, &q.UpdatedAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		cfg.Queues = append(cfg.Queues, CallcenterQueue{Name: q.ID.String(), Queue: q})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	aQ := `
		SELECT qa.queue_id, qa.extension_id, qa.agent_type,
		       qa.tier_level, qa.tier_position,
		       qa.max_no_answer, qa.wrap_up_time, qa.reject_delay_time,
		       qa.busy_delay_time, qa.no_answer_delay_time,
		       e.sip_username, sd.domain
		  FROM queue_agents qa
		  JOIN extensions  e  ON e.id  = qa.extension_id AND e.status = 'active'
		  JOIN sip_domains sd ON sd.id = e.sip_domain_id
		 WHERE qa.enabled = true ` + agentWhere + `
		 ORDER BY qa.queue_id, qa.tier_level, qa.tier_position`
	arows, err := s.DB.Query(ctx, aQ, args...)
	if err != nil {
		return nil, err
	}
	defer arows.Close()

	seenAgents := map[string]bool{}
	for arows.Next() {
		var (
			queueID, extID                      uuid.UUID
			agentType                           string
			tierLevel, tierPos                  int
			maxNA, wrap, rejectD, busyD, noAnsD int
			sipUser, sipDomain                  string
		)
		if err := arows.Scan(
			&queueID, &extID, &agentType, &tierLevel, &tierPos,
			&maxNA, &wrap, &rejectD, &busyD, &noAnsD,
			&sipUser, &sipDomain,
		); err != nil {
			return nil, err
		}
		agentName := "agent_" + extID.String()
		queueName := queueID.String()

		if !seenAgents[agentName] {
			seenAgents[agentName] = true
			cfg.Agents = append(cfg.Agents, CallcenterAgent{
				Name:              agentName,
				Type:              agentType,
				Contact:           "[call_timeout=20]sofia/internal/sip:" + sipUser + "@" + sipDomain + ";fs_path=sip:" + kamailioTarget + ";lr",
				MaxNoAnswer:       maxNA,
				WrapUpTime:        wrap,
				RejectDelayTime:   rejectD,
				BusyDelayTime:     busyD,
				NoAnswerDelayTime: noAnsD,
			})
		}
		cfg.Tiers = append(cfg.Tiers, CallcenterTier{
			Agent:    agentName,
			Queue:    queueName,
			Level:    tierLevel,
			Position: tierPos,
		})
	}
	return cfg, arows.Err()
}

// ListQueuesForTenant returns a tenant's enabled call queues.
func (s *Store) ListQueuesForTenant(ctx context.Context, tenantID uuid.UUID) ([]Queue, error) {
	const q = `
		SELECT id, tenant_id, COALESCE(extension,''), name, strategy, moh_sound,
		       COALESCE(record_template,''), time_base_score,
		       max_wait_time, max_wait_no_agent, max_wait_no_agent_time_reached,
		       tier_rules_apply, tier_rule_wait_second, tier_rule_no_agent_no_wait,
		       discard_abandoned_after, abandoned_resume_allowed,
		       COALESCE(announce_sound,''),
		       enabled, created_at, updated_at
		  FROM queues WHERE tenant_id = $1 AND enabled = true ORDER BY extension NULLS LAST`
	rows, err := s.DB.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Queue
	for rows.Next() {
		var qq Queue
		if err := rows.Scan(
			&qq.ID, &qq.TenantID, &qq.Extension, &qq.Name, &qq.Strategy, &qq.MOHSound,
			&qq.RecordTemplate, &qq.TimeBaseScore,
			&qq.MaxWaitTime, &qq.MaxWaitNoAgent, &qq.MaxWaitNoAgentTimeReached,
			&qq.TierRulesApply, &qq.TierRuleWaitSecond, &qq.TierRuleNoAgentNoWait,
			&qq.DiscardAbandonedAfter, &qq.AbandonedResumeAllowed,
			&qq.AnnounceSound,
			&qq.Enabled, &qq.CreatedAt, &qq.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, qq)
	}
	return out, rows.Err()
}
