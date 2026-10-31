# LAC-03 isolated API acceptance

Scope: deploy an independent NewAPI candidate on LAC-03 and verify real RS and
Chat2API inference with correct wallet, API key and usage-log settlement.
Do not switch customer traffic, change production account state or proxies,
or migrate existing balances and databases.

The application supports `BIND_ADDRESS`, defaulting to its existing wildcard
listen address. Set `BIND_ADDRESS=127.0.0.1` for the acceptance service. Access it
through an authenticated SSH local forward. No public reverse-proxy or DNS
change is required. Build the executable locally and verify the SHA-256 both
before transfer and on the destination.

Use a separate service, state directory, SQLite database, administrator,
customer key and synthetic wallet credit. Disable new checkout methods and
background channel updates. Apply a modest resource budget to this candidate;
the existing RS and Chat2API resource policies and serving instances stay intact.
Real inference must use the existing authenticated upstream endpoints and model
mapping. Read source configuration without copying refresh/access tokens or
account passwords. Never include upstream API secrets in reports or Git.

Before and after deployment, record physical RAM and swap separately, pressure,
disk headroom, application/dependency health, active traffic and serving process
identities. Rollback consists of stopping only the acceptance service after its
own requests drain; it does not touch customer routing or production services.
Preserve its database and small verification manifest.

Predeployment route tests exposed previously undeclared Alipay dashboard routes.
Declare those as session-only, preserving the existing dashboard credential
requirement and rejecting all personal access tokens on these operations. The
signed public payment notification remains outside dashboard authentication.
This does not enable payment or expand payment acceptance scope.

The narrow authorization correction follows default-deny, least privilege and
server-side per-request checks from the [OWASP Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html),
with the [Authentication](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)
and [Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
guidance reviewed against the published stable [ASVS 5.0.0](https://asvs.owasp.org/).
Existing authorization tests cover route completeness and token enforcement;
the focused regression asserts all six Alipay dashboard routes remain session-only.
This is not a claim of complete ASVS certification.
