# secrets/

Per-deploy secrets that mount into containers. **Gitignored** (see `.gitignore`).

## Files expected in production

- `saml-cert.pem` — SP X.509 cert (public)
- `saml-key.pem`  — SP RSA private key

## Generating SAML keys

```bash
go run ./control-plane/cmd/gen-saml-keys "pbx-bigblue-prod" > /tmp/saml.txt
awk '/^---$/{p=1;next} p==0{print > "secrets/saml-cert.pem"} p==1{print > "secrets/saml-key.pem"}' /tmp/saml.txt
chown 65532:65532 secrets/saml-{cert,key}.pem
chmod 644 secrets/saml-cert.pem
chmod 640 secrets/saml-key.pem
```

## Wiring it up

Copy `docker-compose.override.yml.example` → `docker-compose.override.yml`
and `docker compose -p sip-platform up -d --force-recreate control-plane`.
Verify `curl http://your-host:8080/admin/sso/saml/metadata` returns XML.
