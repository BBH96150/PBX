// Package e164 normalizes user-dialed phone numbers into canonical E.164.
//
// Phase 2 supports US as the default country (any 10-digit becomes +1NNNNNNNNNN).
// Phase 3+ will accept ISO-3166 country codes and a richer ruleset (we'll
// likely vendor in github.com/nyaruka/phonenumbers once the surface stabilizes).
package e164

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var validE164 = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

// Normalize returns the canonical E.164 form of `in` (e.g. "+15555551234"),
// or an error if it can't be made sense of for the given default country.
//
// defaultCountry is an ISO-3166 alpha-2 code ("US"). Empty = "US".
//
// Recognized inputs:
//   - "+15555551234"          → "+15555551234"
//   - "0015555551234"         → "+15555551234"  (international 00 prefix)
//   - "15555551234"           → "+15555551234"
//   - "5555551234"            → "+15555551234"  (US default)
//   - "9 1 (555) 555-1234"    → "+15555551234"  (strips trunk-9 + punctuation)
func Normalize(in, defaultCountry string) (string, error) {
	s := strings.TrimSpace(in)
	if s == "" {
		return "", errors.New("empty number")
	}

	var b strings.Builder
	hadPlus := strings.HasPrefix(s, "+")
	if hadPlus {
		b.WriteByte('+')
	}
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	digits := b.String()

	// 00-prefix international dialing → +
	if !hadPlus && strings.HasPrefix(digits, "00") {
		digits = "+" + digits[2:]
	}

	if strings.HasPrefix(digits, "+") {
		if validE164.MatchString(digits) {
			return digits, nil
		}
		return "", fmt.Errorf("invalid E.164: %s", digits)
	}

	cc := strings.ToUpper(strings.TrimSpace(defaultCountry))
	if cc == "" {
		cc = "US"
	}

	switch cc {
	case "US", "CA":
		// Strip the leading PBX trunk-access "9" when the remainder is the
		// right length (10 or 11 digits with leading 1).
		if strings.HasPrefix(digits, "9") && (len(digits) == 11 || len(digits) == 12) {
			digits = digits[1:]
		}
		switch len(digits) {
		case 10:
			digits = "+1" + digits
		case 11:
			if !strings.HasPrefix(digits, "1") {
				return "", fmt.Errorf("invalid 11-digit number for US: %s", digits)
			}
			digits = "+" + digits
		case 7:
			return "", fmt.Errorf("missing area code: %s", digits)
		default:
			return "", fmt.Errorf("cannot normalize %q as US/CA number", digits)
		}
		if validE164.MatchString(digits) {
			return digits, nil
		}
		return "", fmt.Errorf("invalid normalized: %s", digits)
	default:
		return "", fmt.Errorf("default country %q not supported in Phase 2", cc)
	}
}

// LooksLikeExternal returns true if `s` looks like an externally-dialable
// number rather than an internal extension. Used by the dialplan handler to
// pick between local extension lookup and PSTN routing.
//
// Heuristic: starts with "+", or is at least 7 digits long (ignoring + - ( ) space).
func LooksLikeExternal(s string) bool {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "+") {
		return true
	}
	digits := 0
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			digits++
		case r == '-' || r == ' ' || r == '(' || r == ')' || r == '.':
			// punctuation ok
		default:
			return false
		}
	}
	return digits >= 7
}

// DialDigits strips the leading "+" from an E.164 number; some carriers
// (CallCentric) want "1NNNNNNNNNN" rather than "+1NNNNNNNNNN" in the R-URI.
func DialDigits(e164 string) string {
	return strings.TrimPrefix(e164, "+")
}
