// gen-saml-keys mints a self-signed SAML SP keypair for the platform.
// Output is two PEM blocks separated by --- — pipe into your env loader.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/tendpos/sip-platform/control-plane/internal/sso"
)

func main() {
	cn := "sip-platform-sp"
	if len(os.Args) > 1 {
		cn = os.Args[1]
	}
	certPEM, keyPEM, err := sso.GenerateSelfSignedSAMLKeypair(cn, 10*365*24*time.Hour)
	if err != nil {
		fmt.Fprintln(os.Stderr, "err:", err)
		os.Exit(1)
	}
	fmt.Print(string(certPEM))
	fmt.Println("---")
	fmt.Print(string(keyPEM))
}
