package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type IVR struct {
	ID                   uuid.UUID `json:"id"`
	TenantID             uuid.UUID `json:"tenant_id"`
	Name                 string    `json:"name"`
	Extension            string    `json:"extension,omitempty"`
	GreetingLong         string    `json:"greeting_long"`
	GreetingShort        string    `json:"greeting_short"`
	InvalidSound         string    `json:"invalid_sound"`
	ExitSound            string    `json:"exit_sound"`
	TimeoutMS            int       `json:"timeout_ms"`
	InterDigitTimeoutMS  int       `json:"inter_digit_timeout_ms"`
	MaxFailures          int       `json:"max_failures"`
	MaxTimeouts          int       `json:"max_timeouts"`
	DigitLen             int       `json:"digit_len"`
	Enabled              bool      `json:"enabled"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type IVROption struct {
	ID          uuid.UUID  `json:"id"`
	IVRID       uuid.UUID  `json:"ivr_id"`
	Digit       string     `json:"digit"`
	Label       string     `json:"label,omitempty"`
	ActionKind  string     `json:"action_kind"`
	ActionID    *uuid.UUID `json:"action_id,omitempty"`
	ActionData  string     `json:"action_data,omitempty"`
}

type CreateIVRInput struct {
	TenantID  uuid.UUID
	Name      string
	Extension string
	// Optional overrides — if zero/empty, schema defaults apply.
	GreetingLong         string
	GreetingShort        string
	InvalidSound         string
	ExitSound            string
	TimeoutMS            int
	InterDigitTimeoutMS  int
	MaxFailures          int
	MaxTimeouts          int
	DigitLen             int
}

func (s *Store) CreateIVR(ctx context.Context, in CreateIVRInput) (*IVR, error) {
	const q = `
		INSERT INTO ivrs (
			tenant_id, name, extension,
			greeting_long, greeting_short, invalid_sound, exit_sound,
			timeout_ms, inter_digit_timeout_ms,
			max_failures, max_timeouts, digit_len
		) VALUES (
			$1, $2, NULLIF($3,''),
			COALESCE(NULLIF($4,''), 'ivr/ivr-welcome.wav'),
			COALESCE(NULLIF($5,''), 'ivr/ivr-welcome_to_freeswitch.wav'),
			COALESCE(NULLIF($6,''), 'ivr/ivr-that_was_an_invalid_entry.wav'),
			COALESCE(NULLIF($7,''), 'voicemail/vm-goodbye.wav'),
			COALESCE(NULLIF($8, 0), 5000),
			COALESCE(NULLIF($9, 0), 2000),
			COALESCE(NULLIF($10,0), 3),
			COALESCE(NULLIF($11,0), 3),
			COALESCE(NULLIF($12,0), 1)
		)
		RETURNING id, tenant_id, name, COALESCE(extension,''),
		          greeting_long, greeting_short, invalid_sound, exit_sound,
		          timeout_ms, inter_digit_timeout_ms,
		          max_failures, max_timeouts, digit_len,
		          enabled, created_at, updated_at`
	var v IVR
	err := s.DB.QueryRow(ctx, q,
		in.TenantID, in.Name, in.Extension,
		in.GreetingLong, in.GreetingShort, in.InvalidSound, in.ExitSound,
		in.TimeoutMS, in.InterDigitTimeoutMS,
		in.MaxFailures, in.MaxTimeouts, in.DigitLen,
	).Scan(
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

type AddIVROptionInput struct {
	IVRID       uuid.UUID
	Digit       string
	Label       string
	ActionKind  string
	ActionID    *uuid.UUID
	ActionData  string
}

func (s *Store) AddIVROption(ctx context.Context, in AddIVROptionInput) (*IVROption, error) {
	const q = `
		INSERT INTO ivr_options (ivr_id, digit, label, action_kind, action_id, action_data)
		VALUES ($1, $2, NULLIF($3,''), $4, $5, NULLIF($6,''))
		RETURNING id, ivr_id, digit, COALESCE(label,''), action_kind, action_id, COALESCE(action_data,'')`
	var o IVROption
	err := s.DB.QueryRow(ctx, q,
		in.IVRID, in.Digit, in.Label, in.ActionKind, in.ActionID, in.ActionData,
	).Scan(&o.ID, &o.IVRID, &o.Digit, &o.Label, &o.ActionKind, &o.ActionID, &o.ActionData)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// LookupIVRByExtension resolves an internal dialed number (e.g. "300") to an
// IVR for the tenant.
func (s *Store) LookupIVRByExtension(ctx context.Context, tenantDomain, ext string) (*IVR, error) {
	const q = `
		SELECT v.id, v.tenant_id, v.name, COALESCE(v.extension,''),
		       v.greeting_long, v.greeting_short, v.invalid_sound, v.exit_sound,
		       v.timeout_ms, v.inter_digit_timeout_ms,
		       v.max_failures, v.max_timeouts, v.digit_len,
		       v.enabled, v.created_at, v.updated_at
		  FROM ivrs v
		  JOIN sip_domains sd ON sd.tenant_id = v.tenant_id
		 WHERE sd.domain = $1 AND v.extension = $2 AND v.enabled = true
		 LIMIT 1`
	var v IVR
	err := s.DB.QueryRow(ctx, q, tenantDomain, ext).Scan(
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

// LookupDIDIVRTarget resolves an inbound DID whose destination_kind = 'ivr'.
func (s *Store) LookupDIDIVRTarget(ctx context.Context, e164 string) (*IVR, error) {
	const q = `
		SELECT v.id, v.tenant_id, v.name, COALESCE(v.extension,''),
		       v.greeting_long, v.greeting_short, v.invalid_sound, v.exit_sound,
		       v.timeout_ms, v.inter_digit_timeout_ms,
		       v.max_failures, v.max_timeouts, v.digit_len,
		       v.enabled, v.created_at, v.updated_at
		  FROM dids d
		  JOIN ivrs v ON v.id = d.destination_id AND d.destination_kind = 'ivr'
		 WHERE d.e164 = $1 AND d.enabled = true AND v.enabled = true
		 LIMIT 1`
	var v IVR
	err := s.DB.QueryRow(ctx, q, e164).Scan(
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

// IVRMenuRender is what the configuration handler needs to render one
// <menu> XML block: the IVR settings + each option resolved to a callable
// action string.
type IVRMenuRender struct {
	IVR     IVR
	Entries []IVRMenuEntry
}

// IVRMenuEntry is one rendered option: digit + a complete dialplan-app
// string suitable for FS's menu-exec-app, like "transfer 101 XML default".
type IVRMenuEntry struct {
	Digit  string
	Label  string
	Action string // menu-exec-app | menu-back | menu-exit
	Param  string // arg for the action (e.g. "transfer 101 XML default")
}

// ListEnabledIVRMenus returns every enabled IVR with its options pre-rendered
// into menu entries. Called by the configuration handler when FS asks for
// ivr.conf. Wave 3.0 supports action_kind=extension and =hangup; the others
// render as menu-exit (effectively hangup) until Wave 3.5.
func (s *Store) ListEnabledIVRMenus(ctx context.Context) ([]IVRMenuRender, error) {
	const ivrsQ = `
		SELECT id, tenant_id, name, COALESCE(extension,''),
		       greeting_long, greeting_short, invalid_sound, exit_sound,
		       timeout_ms, inter_digit_timeout_ms,
		       max_failures, max_timeouts, digit_len,
		       enabled, created_at, updated_at
		  FROM ivrs WHERE enabled = true`
	rows, err := s.DB.Query(ctx, ivrsQ)
	if err != nil {
		return nil, err
	}
	var ivrs []IVR
	for rows.Next() {
		var v IVR
		if err := rows.Scan(
			&v.ID, &v.TenantID, &v.Name, &v.Extension,
			&v.GreetingLong, &v.GreetingShort, &v.InvalidSound, &v.ExitSound,
			&v.TimeoutMS, &v.InterDigitTimeoutMS,
			&v.MaxFailures, &v.MaxTimeouts, &v.DigitLen,
			&v.Enabled, &v.CreatedAt, &v.UpdatedAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		ivrs = append(ivrs, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]IVRMenuRender, 0, len(ivrs))
	for _, v := range ivrs {
		entries, err := s.fetchIVRMenuEntries(ctx, v.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, IVRMenuRender{IVR: v, Entries: entries})
	}
	return out, nil
}

// IVROptionResolved is the joined view of an ivr_options row with whatever
// data each action_kind needs to render its menu entry. Kept exported so the
// dispatch function (ResolveIVRMenuEntry) is pure and unit-testable.
type IVROptionResolved struct {
	Digit       string
	Label       string
	ActionKind  string
	ActionID    *uuid.UUID
	ActionData  string

	// Pre-joined fields per kind (one will be populated, the others empty):
	ExtNumber       string // extension
	RingGroupExt    string // ring_group
	VoicemailUser   string // voicemail (sip_username)
	VoicemailDomain string // voicemail (sip_domain)
	QueueExt        string // queue.extension
}

func (s *Store) fetchIVRMenuEntries(ctx context.Context, ivrID uuid.UUID) ([]IVRMenuEntry, error) {
	const q = `
		SELECT o.digit, COALESCE(o.label,''), o.action_kind, o.action_id, COALESCE(o.action_data,''),
		       COALESCE(ext.extension, '')      AS ext_number,
		       COALESCE(rg.extension, '')       AS rg_ext,
		       COALESCE(vm_ext.sip_username,'') AS vm_user,
		       COALESCE(vm_sd.domain, '')       AS vm_domain,
		       COALESCE(qq.extension, '')       AS queue_ext
		  FROM ivr_options o
		  LEFT JOIN extensions      ext    ON ext.id  = o.action_id AND o.action_kind = 'extension'
		  LEFT JOIN ring_groups     rg     ON rg.id   = o.action_id AND o.action_kind = 'ring_group'
		  LEFT JOIN voicemail_boxes vb     ON vb.id   = o.action_id AND o.action_kind = 'voicemail'
		  LEFT JOIN extensions      vm_ext ON vm_ext.id = vb.extension_id
		  LEFT JOIN sip_domains     vm_sd  ON vm_sd.id  = vm_ext.sip_domain_id
		  LEFT JOIN queues          qq     ON qq.id   = o.action_id AND o.action_kind = 'queue'
		 WHERE o.ivr_id = $1
		 ORDER BY o.digit`
	rows, err := s.DB.Query(ctx, q, ivrID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IVRMenuEntry
	for rows.Next() {
		var r IVROptionResolved
		if err := rows.Scan(
			&r.Digit, &r.Label, &r.ActionKind, &r.ActionID, &r.ActionData,
			&r.ExtNumber, &r.RingGroupExt, &r.VoicemailUser, &r.VoicemailDomain, &r.QueueExt,
		); err != nil {
			return nil, err
		}
		out = append(out, ResolveIVRMenuEntry(r))
	}
	return out, rows.Err()
}

// ResolveIVRMenuEntry maps a resolved ivr_options row into a mod_ivr <entry>.
// Pure function — no DB access — so it's covered by unit tests.
//
// Mapping per action_kind:
//   extension  → "transfer <ext_num> XML default"
//   ring_group → "transfer <rg_extension> XML default"  (requires rg.extension)
//   voicemail  → "voicemail default <domain> <user>"    (direct mod_voicemail call)
//   ivr        → "ivr <sub_ivr_id>"                     (mod_ivr recurses)
//   dial_e164  → "transfer <+E.164> XML default"        (re-enters dialplan as outbound)
//   hangup     → menu-exit (no app exec)
//
// Any kind that can't be resolved (missing target, removed FK, etc.) falls
// back to menu-exit so the caller hangs up gracefully rather than crashing
// mod_ivr with a malformed entry.
func ResolveIVRMenuEntry(r IVROptionResolved) IVRMenuEntry {
	e := IVRMenuEntry{Digit: r.Digit, Label: r.Label}
	switch r.ActionKind {
	case "extension":
		if r.ExtNumber == "" {
			e.Action = "menu-exit"
			return e
		}
		e.Action = "menu-exec-app"
		e.Param = "transfer " + r.ExtNumber + " XML default"
	case "ring_group":
		// Ring group must have an internal extension number to be IVR-targetable
		// via transfer. The admin should give it one; otherwise we degrade.
		if r.RingGroupExt == "" {
			e.Action = "menu-exit"
			return e
		}
		e.Action = "menu-exec-app"
		e.Param = "transfer " + r.RingGroupExt + " XML default"
	case "voicemail":
		if r.VoicemailUser == "" || r.VoicemailDomain == "" {
			e.Action = "menu-exit"
			return e
		}
		e.Action = "menu-exec-app"
		e.Param = "voicemail default " + r.VoicemailDomain + " " + r.VoicemailUser
	case "ivr":
		if r.ActionID == nil {
			e.Action = "menu-exit"
			return e
		}
		e.Action = "menu-exec-app"
		e.Param = "ivr " + r.ActionID.String()
	case "dial_e164":
		if r.ActionData == "" {
			e.Action = "menu-exit"
			return e
		}
		// action_data is stored normalized (+1NNNNNNNNNN). transfer re-enters
		// dialplan; our handler sees E.164 and routes outbound via carrier.
		e.Action = "menu-exec-app"
		e.Param = "transfer " + r.ActionData + " XML default"
	case "queue":
		// Queue must have an extension number for IVR-via-transfer to work.
		if r.QueueExt == "" {
			e.Action = "menu-exit"
			return e
		}
		e.Action = "menu-exec-app"
		e.Param = "transfer " + r.QueueExt + " XML default"
	case "hangup":
		e.Action = "menu-exit"
	default:
		e.Action = "menu-exit"
	}
	return e
}
