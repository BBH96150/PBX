package freeswitch

import (
	"strings"
	"testing"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func render(t *testing.T, a store.CarrierAccount) string {
	t.Helper()
	xml, err := renderGatewayXML(a)
	if err != nil {
		t.Fatalf("renderGatewayXML: %v", err)
	}
	return xml
}

// CallCentric is the canonical case where the proxy host and the auth realm
// differ — the realm is baked into the digest hash, so it must come from the
// carrier default, not the proxy host.
func TestRenderGatewayXML_CallCentricRealm(t *testing.T) {
	xml := render(t, store.CarrierAccount{
		FSGatewayName: "acme-cc", SIPUsername: "17771234567", SIPPassword: "pw",
		CarrierKind: "callcentric", CarrierProxyHost: "sip.callcentric.net",
		CarrierDefaultAuthRealm: "callcentric.com", Register: true,
	})
	for _, want := range []string{
		`<gateway name="acme-cc">`,
		`<param name="username" value="17771234567"/>`,
		`<param name="password" value="pw"/>`,
		`<param name="realm" value="callcentric.com"/>`,
		`<param name="from-domain" value="callcentric.com"/>`,
		`<param name="proxy" value="sip.callcentric.net"/>`,
		`<param name="register" value="true"/>`,
		`<param name="register-transport" value="udp"/>`, // default
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("gateway XML missing %q\n---\n%s", want, xml)
		}
	}
}

func TestRenderGatewayXML_Overrides(t *testing.T) {
	xml := render(t, store.CarrierAccount{
		FSGatewayName: "acme-tx", SIPUsername: "u", SIPPassword: "p",
		CarrierProxyHost: "carrier.example.com", CarrierTransport: "udp",
		ProxyHostOverride: "myproxy.example.com", ProxyPortOverride: 5080,
		AuthRealm: "myrealm.example.com", TransportOverride: "tcp", Register: false,
	})
	for _, want := range []string{
		`<param name="proxy" value="myproxy.example.com:5080"/>`, // host + port override
		`<param name="realm" value="myrealm.example.com"/>`,      // explicit realm wins
		`<param name="register" value="false"/>`,
		`<param name="register-transport" value="tcp"/>`, // transport override
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("gateway XML missing %q\n---\n%s", want, xml)
		}
	}
}

func TestRenderGatewayXML_RealmFallsBackToProxy(t *testing.T) {
	// No explicit realm and no carrier default → realm falls back to the proxy host.
	xml := render(t, store.CarrierAccount{
		FSGatewayName: "g", SIPUsername: "u", SIPPassword: "p",
		CarrierProxyHost: "proxy.example.net", Register: true,
	})
	if !strings.Contains(xml, `<param name="realm" value="proxy.example.net"/>`) {
		t.Errorf("realm should fall back to proxy host:\n%s", xml)
	}
}
