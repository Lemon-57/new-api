# Trusted ingress billing

This optional policy selects the effective group multiplier from an
authenticated serving edge. Ordinary traffic uses 0.2, optimized traffic 0.22,
and the configured image exception uses 1. Model base prices, API keys, funding
preferences and balances are not rewritten. Legacy Sub2API subscription quota
exchange rules are outside this feature.

The default is disabled. Set NEWAPI_BILLING_INGRESS_ROUTES to a JSON object:

    {
      "ordinary": {"group_multiplier": 0.2, "image_multiplier": 1,
                   "image_models": ["gpt-image-2.5"]},
      "optimized": {"group_multiplier": 0.22, "image_multiplier": 1,
                    "image_models": ["gpt-image-2.5"]}
    }

NEWAPI_BILLING_INGRESS_TOKEN must contain a private random token of at least
32 characters. Keep it outside Git, model expressions and public option APIs.
NEWAPI_BILLING_INGRESS_TRUSTED_CIDRS defaults to 127.0.0.1/32,::1/128.
The edge must overwrite X-NewAPI-Billing-Ingress and
X-NewAPI-Billing-Ingress-Token, regardless of caller-provided values.

The application checks the actual socket peer and token. Forwarded IP and Host
do not establish trust. Missing, duplicate, unknown or unauthenticated ingress
metadata rejects relay requests with 403 before upstream dispatch. Both headers
are erased before logging, request snapshots and upstream relay.

The edge must also bind each public role to the intended TLS SNI and Host.
An optimized front must use a fixed authenticated upstream destination and
canonical upstream hostname; it must not forward a caller-selected ordinary
role. Backend listeners remain private. These deployment controls are necessary;
setting a caller-readable route header alone is not the feature.

The immutable request-context snapshot overrides the normal and special group
ratios at the existing pricing boundary, before reservation. The existing price
data and billing snapshot then carry the effective ratio through settlement,
refunds, retries, tool fees and task funding. Image API requests and configured
image pricing/routing names use the independent multiplier. Add image aliases
to the policy when registering additional aliases, including text-protocol
routes to image models.

Every Responses WebSocket turn receives the original authenticated snapshot
when its per-turn Gin context is recreated. It does not re-read client headers
or infer trust from that internal request's rewritten RemoteAddr.
Synchronous consume logs record billing_ingress alongside the effective
group_ratio. Existing asynchronous funding snapshots retain their numeric
group ratio; no database schema or funding-unit conversion is introduced.

The focused contract tests live in controller/responses_websocket_test.go.
They exercise real wallet and bounded-token reservation/settlement, repeated
settlement, independent image charges, image aliases, two fresh WS turn
contexts and rejected forged metadata. TEST_RESPONSES_SQL_DSN selects a
disposable MySQL or PostgreSQL database; unset uses real SQLite.

Build only on the workstation. tools/deployment/binary_delta.py can transport a
locally built binary using exact copies from a verified previous artifact.
The pack command performs a full local reconstruction/hash check. Applying
checks the old artifact and final artifact hashes and creates a separate output
file; it does not compile code or overwrite a running executable.

This narrow edge-authentication control follows default-deny, server-side
authorization and credential-safe logging principles from the
[OWASP Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html)
and [Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html).
The reviewed stable baseline is ASVS 5.0.0. This is not full ASVS certification.
