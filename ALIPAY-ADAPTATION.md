# Alipay face-to-face adaptation

Status: provider implementation verified locally; New API checkout, durable
fulfillment, administrative configuration and browser acceptance remain pending.
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

Next acceptance gates: persist immutable orders and refund identities; atomically
fulfill wallet/subscription orders once; test simultaneous/late callbacks and crash
recovery; verify fresh and upgraded SQLite/MySQL/PostgreSQL databases twice; add
configuration and checkout UI; compile and run an isolated local candidate; then
perform a separately reviewed human payment and refund.

Security references used during implementation: [OWASP payment gateway integration](https://cheatsheetseries.owasp.org/cheatsheets/Third_Party_Payment_Gateway_Integration_Cheat_Sheet.html),
[authentication](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)
and [session management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html).
Order fulfillment, durable replay protection and authenticated configuration are
still integration gates; these provider tests do not establish ASVS compliance.
