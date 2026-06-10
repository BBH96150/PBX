package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// BlockedNumber is a single entry on a tenant's inbound call-screening blocklist
// (see migration 0041). Inbound PSTN calls whose caller-ID matches Number are
// rejected in the dialplan before they ring anyone. Label is the optional reason
// (e.g. "spam", "robocaller").
type BlockedNumber struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Number    string    `json:"number"`
	Label     string    `json:"label,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateBlockedNumberInput struct {
	TenantID uuid.UUID
	Number   string
	Label    string
}

func (s *Store) CreateBlockedNumber(ctx context.Context, in CreateBlockedNumberInput) (*BlockedNumber, error) {
	const q = `
		INSERT INTO blocked_numbers (tenant_id, number, label)
		VALUES ($1, $2, NULLIF($3,''))
		RETURNING id, tenant_id, number, COALESCE(label,''), created_at, updated_at`
	var b BlockedNumber
	err := s.DB.QueryRow(ctx, q, in.TenantID, in.Number, in.Label).Scan(
		&b.ID, &b.TenantID, &b.Number, &b.Label, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Store) ListBlockedNumbersForTenant(ctx context.Context, tenantID uuid.UUID) ([]BlockedNumber, error) {
	const q = `
		SELECT id, tenant_id, number, COALESCE(label,''), created_at, updated_at
		  FROM blocked_numbers WHERE tenant_id = $1
		 ORDER BY number`
	rows, err := s.DB.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BlockedNumber
	for rows.Next() {
		var b BlockedNumber
		if err := rows.Scan(
			&b.ID, &b.TenantID, &b.Number, &b.Label, &b.CreatedAt, &b.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// DeleteBlockedNumberForTenant removes a blocklist entry, tenant-scoped.
func (s *Store) DeleteBlockedNumberForTenant(ctx context.Context, tenantID, id uuid.UUID) error {
	const q = `DELETE FROM blocked_numbers WHERE id = $1 AND tenant_id = $2`
	ct, err := s.DB.Exec(ctx, q, id, tenantID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrCrossTenant
	}
	return nil
}

// IsCallerBlocked reports whether callerNumber is on the blocklist of the tenant
// that owns tenantDomain (a sip_domain). Used by the FS xml_curl dialplan handler
// to screen inbound PSTN calls.
//
// Matching: we compare two ways and block on either —
//
//  1. exact string match (stored number == caller), and
//  2. last-10-digits match: both sides are stripped to their bare digits and
//     compared on their final 10 digits, so "+1XXXXXXXXXX" and "XXXXXXXXXX" (and
//     "1XXXXXXXXXX") all block. This is the common North-American case where a
//     trunk may or may not present the +1 / 1 prefix. The last-10 comparison only
//     fires when both sides have at least 10 digits, so short/internal numbers
//     don't false-match.
//
// Returns false (not blocked) when there is no match, when the domain/caller is
// empty, or when no row exists — never an error for "not blocked". A non-nil
// error only signals a genuine query failure (callers should fail OPEN on it).
func (s *Store) IsCallerBlocked(ctx context.Context, tenantDomain, callerNumber string) (bool, error) {
	if tenantDomain == "" || callerNumber == "" {
		return false, nil
	}
	// digits($1) := the caller's bare digits; we strip non-digits from both the
	// caller and each stored number and compare the trailing 10 digits.
	const q = `
		SELECT EXISTS (
			SELECT 1
			  FROM blocked_numbers bn
			  JOIN sip_domains sd ON sd.tenant_id = bn.tenant_id
			 WHERE sd.domain = $1
			   AND (
			        bn.number = $2
			        OR (
			            length(regexp_replace($2, '\D', '', 'g')) >= 10
			            AND length(regexp_replace(bn.number, '\D', '', 'g')) >= 10
			            AND right(regexp_replace(bn.number, '\D', '', 'g'), 10)
			              = right(regexp_replace($2, '\D', '', 'g'), 10)
			        )
			   )
		)`
	var blocked bool
	if err := s.DB.QueryRow(ctx, q, tenantDomain, callerNumber).Scan(&blocked); err != nil {
		return false, err
	}
	return blocked, nil
}

// TenantDomainForDID resolves the primary sip_domain of the tenant that owns the
// DID with the given E.164 number. Used by the inbound dialplan to key the
// blocklist check (IsCallerBlocked) before routing the call. Returns pgx.ErrNoRows
// if the DID is unknown or the tenant has no primary domain.
func (s *Store) TenantDomainForDID(ctx context.Context, e164 string) (string, error) {
	const q = `
		SELECT sd.domain
		  FROM dids d
		  JOIN sip_domains sd ON sd.tenant_id = d.tenant_id AND sd.is_primary = true
		 WHERE d.e164 = $1
		 LIMIT 1`
	var domain string
	if err := s.DB.QueryRow(ctx, q, e164).Scan(&domain); err != nil {
		return "", err
	}
	return domain, nil
}
