package freeswitch

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// eventLike is the subset of *eslgo.Event we use, narrowed so tests can
// supply a fake without depending on eslgo internals.
type eventLike interface {
	GetName() string
	GetHeader(string) string
}

// shouldRecordCDR returns true for the A-leg of a CHANNEL_HANGUP_COMPLETE
// event. Without this filter we'd write a CDR per leg (two rows per call).
//
// FreeSWITCH sets Caller-Direction=inbound on the originating (A) leg
// regardless of whether the call started from a user or a carrier. The B-leg
// gets Direction=outbound. We want one row per call → A-leg only.
func shouldRecordCDR(e eventLike) bool {
	if e.GetName() != "CHANNEL_HANGUP_COMPLETE" {
		return false
	}
	return strings.EqualFold(e.GetHeader("Caller-Direction"), "inbound")
}

// eventToCDR maps a CHANNEL_HANGUP_COMPLETE event into a *store.CDR.
// All timing fields are best-effort; FreeSWITCH may omit them on early hangups.
func eventToCDR(e eventLike) *store.CDR {
	get := e.GetHeader

	cdr := &store.CDR{
		CallUUID:      get("Unique-ID"),
		Direction:     normalizeDirection(get("variable_x_call_direction")),
		FromURI:       firstNonEmpty(decodeFSValue(get("variable_sip_from_uri")), get("Caller-Caller-ID-Number")),
		ToURI:         firstNonEmpty(decodeFSValue(get("variable_sip_to_uri")), get("Caller-Destination-Number")),
		CallerIDNum:   get("Caller-Caller-ID-Number"),
		CallerIDName:  decodeFSValue(get("Caller-Caller-ID-Name")),
		HangupCause: get("Hangup-Cause"),
		// Wave 5.0: prefer the channel var we set explicitly, then FS's own
		// post-record vars as fallback.
		RecordingPath: firstNonEmpty(
			get("variable_recording_path"),
			get("variable_record_file_path"),
			get("variable_record_filename"),
		),
		Raw: collectVarHeaders(e),
	}

	if t := parseEpoch(get("variable_start_epoch")); !t.IsZero() {
		cdr.StartedAt = t
	} else {
		// Fall back to event timestamp so the row is always insertable.
		cdr.StartedAt = time.Now()
	}
	if t := parseEpoch(get("variable_answer_epoch")); !t.IsZero() {
		cdr.AnsweredAt = &t
	}
	if t := parseEpoch(get("variable_end_epoch")); !t.IsZero() {
		cdr.EndedAt = &t
	}

	if v := get("variable_duration"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cdr.DurationSec = &n
		}
	}
	if v := get("variable_billsec"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cdr.BillableSec = &n
		}
	}
	if v := get("variable_originate_disposition"); v != "" {
		d := normalizeDisposition(v)
		cdr.Disposition = &d
	}
	if v := get("variable_x_tenant_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			cdr.TenantID = &id
		}
	}
	// Outbound calls stamp x_carrier_account_id; inbound stamp x_did_id (carrier
	// resolved at write time would need a join — Phase 3 enhancement).
	if v := get("variable_x_carrier_account_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			// We store the carrier_account_id under carrier_id for now; it's a
			// 1:1 join, and a clean schema migration can rename later.
			cdr.CarrierID = &id
		}
	}

	return cdr
}

// normalizeDirection maps our channel-var values to the CDR enum. Empty maps
// to "internal" as the most innocuous default.
func normalizeDirection(v string) string {
	switch strings.ToLower(v) {
	case "inbound":
		return "inbound"
	case "outbound":
		return "outbound"
	case "internal":
		return "internal"
	default:
		return "internal"
	}
}

func normalizeDisposition(v string) string {
	switch strings.ToUpper(v) {
	case "ANSWERED", "ANSWER":
		return "ANSWERED"
	case "NO_ANSWER", "NOANSWER":
		return "NO_ANSWER"
	case "BUSY", "USER_BUSY":
		return "BUSY"
	case "CANCEL", "CANCELED", "CANCELLED", "ORIGINATOR_CANCEL":
		return "CANCELLED"
	case "CONGESTION":
		return "CONGESTION"
	default:
		return "FAILED"
	}
}

func parseEpoch(s string) time.Time {
	if s == "" || s == "0" {
		return time.Time{}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(n, 0).UTC()
}

// decodeFSValue undoes FreeSWITCH's URL-style escaping (e.g. "Smith%20J" → "Smith J").
func decodeFSValue(s string) string {
	if s == "" {
		return s
	}
	if d, err := url.QueryUnescape(s); err == nil {
		return d
	}
	return s
}

// collectVarHeaders keeps only the variable_* headers in the raw payload —
// the channel-data headers (Caller-*, Channel-*) are huge and we already
// extract the few we care about above.
func collectVarHeaders(e eventLike) map[string]string {
	// We can't iterate event headers via eventLike; the caller must populate.
	// Keep nil; the writer fills it from the real eslgo.Event.
	return nil
}
