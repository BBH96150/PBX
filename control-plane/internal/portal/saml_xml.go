package portal

import "encoding/xml"

// crewjamMarshal indents the SP metadata so it's readable when an IdP admin
// pastes it. Centralized here so saml.go stays focused on flow.
func crewjamMarshal(v any) ([]byte, error) {
	return xml.MarshalIndent(v, "", "  ")
}
