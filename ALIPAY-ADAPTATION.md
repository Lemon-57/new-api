# Alipay face-to-face adaptation

Status: provider, checkout HTTP handlers and durable wallet/subscription
fulfillment verified locally. Managed configuration and its form have automated
coverage; browser payment acceptance and refund accounting remain pending.
This branch is not ready to replace an existing payment service.

Baseline: New API `v1.0.0-rc.47` (`e835b1bfc43b565d625537830dbfb19328925134`).
Reference: Sub2API's `backend/internal/payment/provider/alipay.go` at
`ccd460d9c584d114387675dad397b7c2f7d7b322`, using the same
`github.com/smartwalle/alipay/v3` SDK version, `v3.2.29`.

The adapter preserves desktop precreate with page-pay fallback, explicit page-pay
mode, mobile WAP and optional mobile precreate. Only precreate payloads populate
the QR field. It includes trade query/close, RSA2 notification verification,
refund submission and refund reconciliation with the reference's ten-minute
not-found grace period.

Additional boundaries:

- Compare the immutable merchant order number and exact CNY total in integer
  cents. Do not substitute discounted buyer or receipt amounts for order totals.
- Check application identity, configured seller identity, transaction identity,
  currency when present, duplicate form fields, and supported signature mode.
- Prevent an unauthenticated certificate serial from initiating SDK certificate
  downloads in this public-key integration.
- Require a caller-supplied durable refund request number. Reuse it after any
  uncertain result; never generate a fresh refund ID during retry.
- Inspect signed business error responses before treating query, close or refund
  as successful. Unknown refund outcomes remain pending.
- Return non-secret errors; never log credential configuration or callback bodies.

Local validation (Windows, Go 1.27.3):

```text
go test ./pkg/payment/alipay -count=1 -v
```

Five test functions passed. The local HTTP fixture verifies the merchant's actual
RSA2 requests and signs SDK responses with ephemeral test keys. Tests cover routing,
amount bounds, signed mismatches, tampered signatures, duplicate notification
fields, non-payment states, signed query/close failures, refund retry identity,
amount mismatches and refund reconciliation. No live payment was initiated.

The integration persists an `AlipayOrder` with exact CNY cents, credit units,
application/seller identity, a per-user request identity and a plan snapshot.
Native top-up/subscription orders are created in the same transaction. A nullable
unique provider transaction number prevents one payment from fulfilling two orders.
Completion locks the order and updates its native receipt and wallet/subscription
atomically. Existing quota safety checks and cache delta paths are reused.

Refund preparation now persists an `AlipayRefundRecord` before calling the
provider. The same order/amount/reason returns the same `refund_request_id` after
a timeout or restart; changing the amount is rejected. State transitions are
locked and idempotent for `pending -> refunded/failed`. The service layer exposes
provider submission and reconciliation internally; pending responses remain pending,
queries use current time for the absence grace period, and partial refunds retain
the original order total. Disabling new checkout does not disable reconciliation; no user-facing refund button
or automatic wallet reversal is enabled yet, because reversing already-consumed
quota requires a separate business policy and review.

Routes:

- `POST /api/user/alipay/pay`: authenticated wallet checkout.
- `POST /api/subscription/alipay/pay`: authenticated plan checkout.
- `GET /api/user/alipay/orders/:order_id`: owner-only payment reconciliation.
- `GET /api/user/alipay/orders`: owner-scoped recent-order recovery list.
- `POST /api/user/alipay/notify`: bounded RSA2-verified notification followed by
  an independent signed gateway query. Browser returns never fulfill orders.

Payment creation requires the existing payment compliance gate. Request bodies
carry `request_id` and either `amount` or `plan_id`. Retry with the same request
identity after an uncertain result. The original amount/plan and merchant binding
must match. A committed order is returned without creating another checkout.

For isolated local evaluation, `ALIPAY_DIRECT_CONFIG_FILE` points to a private
JSON file outside the source checkout. It uses Sub2API credential names plus
`enabled` and optional `cnyPerUsd` (a decimal string). USD plans require an explicit
rate; CNY plans retain their stored price. Set `notifyUrl` to the candidate's
`/api/user/alipay/notify` URL and use its own return URL. Never copy a production
callback destination into a local candidate. No credential is returned by these
APIs or added to configuration exports. The private-file override takes precedence
over managed configuration and its presence rejects managed saves.

Database/HTTP validation on Windows, Go 1.27.3:

```text
go test ./model -run '^TestAlipayOrderDatabaseMatrix' -timeout 180s -count=1 -v
go test ./model -run '^TestAlipayOrderProcessRecovery$' -timeout 45s -count=1 -v
go test ./controller -run '^TestAlipayDirectCheckoutAndNotification$' -timeout 45s -count=1 -v
```

The matrix was run against real SQLite 3.50.4, PostgreSQL 18.6 and MySQL 8.0.44.
Each engine covered a fresh database and a representative rc.47 schema upgrade,
two actual startup migrations, legacy balance/receipt preservation, simultaneous
callbacks, provider transaction uniqueness, rollback after native receipt failure,
final wallet ceiling enforcement, pending-plan purchase limits and immutable plan
fulfillment. SQLite was also tested with a single connection; this exposed and
fixed a pre-existing nested database-time query that could deadlock fulfillment.
The matrix was rerun after adding `AlipayRefundRecord`; all six fresh/upgrade
runs passed.

A separate process test commits a wallet payment, exits before acknowledging the
caller, then starts a second process against that database: the retry is idempotent
and the balance and receipt remain single. A Redis fixture test verifies that
credit preserves an in-flight reservation and duplicate callbacks do not increase
cache credit twice. This does not prove recovery from a crash between the SQL
commit and cache synchronization; that boundary remains an explicit test gate.

HTTP tests cover repeated request identities, changed retry amounts, signed
notifications whose independent query is still pending, repeat paid notifications,
duplicate form fields, lost notification recovery for a plan, no wallet credit on
plan purchase, native receipt units when the request uses token display units,
and cross-user query rejection before any gateway call. Currency tests cover
explicit USD conversion, unchanged CNY totals, cent rounding and invalid totals.

The isolated fixture gateway (`tools/alipay-lab/fixture_gateway.py`) implements
the signed SDK request/response envelope on a generated local CA and keeps orders
in a private runtime directory. A probe through the compiled candidate created a
CNY 7.20 QR checkout, marked it paid through the local fixture, delivered the
signed notification, and read back `success`; the first probe increased wallet
quota to 1,000,000 and repeated reconciliation stayed idempotent. A second probe
created a plan checkout, completed it through the fixture, and read back one active
subscription. These are synthetic local payments; no live Alipay endpoint or
production credential was used.

The Edge page rendered the Alipay method, recent-order card, and active local
subscription on the compiled candidate. The Playwright extension click action
timed out while waiting for the rendered payment button to become stable, despite
no application console error and a passing API flow. QR rendering and the actual
human click/scan therefore remain unverified in the browser; this is recorded as
an acceptance gap rather than inferred from the successful HTTP probe.

Remaining acceptance gates: external refund/close accounting and wallet-reversal
policy;
late, reordered and refunded trade handling; SQL-to-Redis crash recovery; native
runtime acceptance of managed configuration; complete browser payment tests;
then a separately reviewed human payment and refund. Existing protocol, billing,
migration and observation gaps in the broader replacement assessment also remain.

Security references used during implementation: [OWASP payment gateway integration](https://cheatsheetseries.owasp.org/cheatsheets/Third_Party_Payment_Gateway_Integration_Cheat_Sheet.html),
[authentication](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)
and [session management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html).
Refund fulfillment, cache crash recovery and authenticated configuration are
still integration gates; these tests do not establish ASVS compliance.

## Managed configuration acceptance

The existing payment settings page now includes an Alipay tab. Saving it sends
one complete request to `PUT /api/option/alipay`, under the existing root-only
router plus a dashboard-session check. Personal API tokens are rejected.
Enabling requires the existing compliance gate, valid signing/verification keys,
and a callback URL. HTTPS is required except for loopback HTTP in sandbox mode.
USD-to-CNY conversion is explicit; there is no silently selected default rate.

The complete configuration is committed as one `AlipayDirectConfigSecret` row in
the existing options table, rather than independent per-field writes. Payment
readers use that committed row. Blank key inputs preserve the stored keys, and
ordinary option updates cannot bypass this endpoint. General settings reads expose
only the non-secret fields. Save logging records the actor and mode flags, excludes
keys, and suppresses ORM SQL diagnostics for secret-bearing reads/writes. The
stored value uses the same database storage convention as existing payment
secrets; this change does not add encryption at rest.

Merchant/application, sandbox and verification-key changes are conservatively
rejected once orders exist. Safe rotation across historical orders needs a
versioned merchant/key strategy and remains a limitation. New order creation
locks the managed configuration row before its merchant snapshot so it cannot
race a merchant change.

Automated settings checks cover API-token rejection, blank-key preservation,
failed-save rollback, generic-setting bypass rejection, read redaction and form
save success/failure. The form submits one complete save, masks key fields,
preserves pasted PEM line breaks, clears entered keys only after success, and
supports all seven locale files. The related frontend suites contain 25 passing
tests. Refund-service regression checks exercise signed pending, absent and
successful responses plus a terminal retry with no second provider submission.

These settings and refund-service fixes have not been installed into the running
v2 browser candidate. A successful test/build does not establish live Alipay
merchant acceptance, complete refund accounting, or replacement readiness.
