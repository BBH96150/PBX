// A complete, runnable webhook receiver for the SIP platform (no dependencies).
//
// Verifies the HMAC-SHA256 signature on every delivery, then dispatches events.
// Set WEBHOOK_SECRET to the signing secret from the portal (Tenant -> Webhooks)
// and point your endpoint URL at this server (expose it over HTTPS, e.g. via a
// tunnel — the platform only delivers to public HTTPS URLs).
//
//	WEBHOOK_SECRET=... go run webhook-receiver.go
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

func main() {
	secret := os.Getenv("WEBHOOK_SECRET")
	if secret == "" {
		log.Fatal("set WEBHOOK_SECRET to your endpoint's signing secret")
	}
	http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body) // the EXACT bytes — verify over these
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(raw)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expected), []byte(r.Header.Get("X-Webhook-Signature"))) {
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}

		var env struct {
			Event string         `json:"event"`
			Data  map[string]any `json:"data"`
		}
		_ = json.Unmarshal(raw, &env)

		switch env.Event { // call.completed, trunk.down/up, voicemail.new, test.ping
		case "call.completed":
			fmt.Printf("call %v %v -> %v %vs %v\n",
				env.Data["direction"], env.Data["from"], env.Data["to"],
				env.Data["duration_sec"], env.Data["disposition"])
		case "trunk.down", "trunk.up":
			fmt.Printf("trunk %v %v -> %v\n", env.Data["trunk"], env.Data["prev_state"], env.Data["state"])
		case "voicemail.new":
			fmt.Printf("new voicemail for ext %v from %v\n", env.Data["extension_id"], env.Data["caller_id_num"])
		case "test.ping":
			fmt.Println("test ping received — endpoint is wired up correctly")
		default:
			fmt.Printf("unhandled event: %s\n", env.Event)
		}

		w.WriteHeader(http.StatusNoContent) // return 2xx fast; non-2xx triggers retry
	})

	addr := ":8088"
	log.Printf("listening on %s/webhook", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
