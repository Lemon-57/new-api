# JEV automatic request moderation and key pool

Extends request checks with TypeSafe JEV pre-request moderation. Legacy single-key configuration remains readable. The encrypted `ContentModerationAPIKey` option stores a normalized newline-delimited pool, with up to 100 unique keys. No database schema changes.

## Behavior

- One key per line; explicit append/replace operations. Blank input preserves credentials. Delete by SHA256 identifier; deleting the last key follows the unavailable-service policy.
- Mutex-protected round-robin selection on each attempt. Frozen keys are skipped. Retry budget remains retry_count + 1 (maximum 6); cancellation interrupts backoff.
- Sub2API freeze policy: HTTP 401/403 10 minutes; 429/529 1 minute; other HTTP statuses except 400 10 seconds. Transport failure has no freeze; HTTP 400 stops retries.
- Per-key counters and health remain process-local and reset at restart. Only masked identifiers are returned. Credentials use AES-GCM, persisted deployment secret and random nonces.
- Successful JEV risk decisions return HTTP 403 with content_policy_violation. Exhausted keys/provider failures honor ContentModerationFailOpen. Existing keyword/auth/quota rules apply independently.

## Verification (2026-11-08)

Go 1.26.10 focused tests passed in setting/service/model/controller/relay, including multikey failover, freeze expiry, concurrent rotation, encrypted reload, append/delete/blank preserve and Responses WebSocket rejection. Configuration transaction matrix passed on SQLite 3.50.4, MySQL 8.4.0 and PostgreSQL 18.6; MariaDB 11.8.9 also passed. Typecheck: 1416 files, 2 projects. Settings UI suite: 21 passed; existing routing/layout tests: 38 passed. Changed UI files passed oxlint. Production frontend build passed.

Production artifact v1.0.0-rc.49-alipay-moderation3: SHA256 6aa4f0c1abe4b6d41fcbb6c61eef5eeeb1fc776464a54735575cf5f756aada92. Real https://jev.bocha.cn evaluation switched from a failing synthetic key to the existing valid key in an isolated process and returned risk scores. Public production request returned 403/content_policy_violation; selected token used_quota unchanged. Production configuration retained one valid key and fail-open=true. No synthetic key was stored in production.

## Limits and operational notes

This change aligns JEV key-pool behavior; it does not claim parity with every Sub2API risk-control feature. No production outage injection was performed; unavailable-provider behavior was tested locally. Browser acceptance observed the new key/status UI before the final translation fix; final visual readback was blocked by extension timeouts. The broad pre-existing routing SQLite suite hit a Windows audit.db cleanup lock once; focused moderation controller tests passed.

Deployment used a temporary slave node, switched new requests, waited for zero old connections and then replaced the master artifact. The candidate was stopped after draining. A stale Docker file bind made the first Caddy reload read an older configuration and briefly fail TLS; the complete intended configuration was restored and final cutovers changed only the runtime upstream value. Future proxy reloads must use a fresh config path or the verified admin configuration; the original read-only file mount still references a stale inode. Prior binary and full runtime configuration retained for rollback.

Security references: OWASP Authentication Cheat Sheet, Session Management Cheat Sheet and Secrets Management Cheat Sheet. Applied TLS-only external endpoint validation, no credential forwarding through redirects, authenticated existing admin route, encrypted storage and masked response/telemetry. No ASVS certification claim.
