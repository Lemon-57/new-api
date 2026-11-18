# Direct ingress and graceful application updates

## Steady state

Use the existing Nginx or Caddy edge as the last trusted proxy. The ordinary
entrance reaches the application host directly. The optimized entrance reaches
the same host through an approved, source-restricted private connection. Both
entrances use the same application's keys and authoritative billing store.

Do not retain a separate application-switch process solely to support route
pricing or ordinary releases. A temporary migration coordinator may be useful
for a specifically approved ledger handover, but it has its own retirement gate.

## Trusted route identity

- Determine the route from the real listener and authenticated private peer.
  Never select a cheaper route using client-supplied Host, forwarded IPs, or
  billing headers.
- Restrict the optimized private listener to the approved peer, at both the
  host firewall and proxy request matcher.
- Overwrite duplicate or forged route headers. For New API, also overwrite the
  private proof header and retain application checks on the socket peer and
  proof. Keep the proof in protected deployment configuration; never commit it.
- Ordinary requests use factor 1; optimized requests use factor 1.1. A request
  reaching the ordinary backup directly uses factor 1 even if its public
  hostname normally selects the optimized entrance.
- Preserve existing model, user, group, independent media, fixed-price, and
  subscription rules. Removing a proxy must not broaden the surcharge scope.
  Verify the application's actual media and per-request exceptions.
- Preserve request-scoped pricing snapshots through deferred settlement,
  retries, and WebSocket turns. Do not write route-specific prices to shared
  user or group caches.

## Release procedure

1. Inventory actual applications, domains, private callers, OAuth callbacks,
   payment paths, image routes, database/cache dependencies, and DNS failover.
   Check host and per-container headroom, current traffic, active connections,
   dependency health, and application resource policies.
2. Build and test on the workstation. Verify artifact checksums locally and
   after transfer. Do not build or install build dependencies on application
   servers. Reuse the verified artifact where compatible.
3. Start and validate an isolated candidate with a verified rollback path.
   Prevent duplicate background jobs and unintended production writes. Check
   billing identity, spoofed and duplicate headers, ordinary and optimized
   routes, and application rejection of untrusted direct calls.
4. Validate proxy configuration before changing traffic. Exercise HTTP, SSE,
   and WebSocket continuity across a genuine graceful reload in isolation.
   Keep stream flushing enabled and avoid forced worker/stream expiry.
5. Preserve special routes and unrelated services. Gracefully load the new
   upstream configuration through the existing proxy, directing new requests
   to the candidate. Existing accepted requests keep their original upstream.
   Do not restart the shared edge or either application to perform the switch.
6. Read back the proxy's effective configuration and confirm real requests,
   semantic completion, billing receipts, errors, latency, dependencies, and
   resource usage. A successful reload command or one healthy endpoint is not
   sufficient verification.
7. Observe old instances until their active requests, queued/background work,
   and established connections are repeatedly zero. Preserve long streams;
   elapsed time is not permission to interrupt them. Stop and disable only
   dedicated, fully drained instances. Retain current rollback material and
   protect the authoritative ledger.
8. Keep boot configuration consistent with the verified live route. Remove
   consumed redundant local build outputs after deployment verification, while
   retaining source, checksums, receipts, dependencies, and necessary recovery
   artifacts. Report verified reclaimed bytes and remaining disk space.

## Single-file bind mounts

Replacing a host configuration file by rename can leave a running container
bound to a deleted, older inode. The host file may look correct while a reload
inside the container still reads the old configuration.

Compare the host file, the mounted file, and the effective proxy configuration.
For Caddy, read back the explicit local administrator endpoint after loading the
validated configuration. Keep the protected on-disk and mounted configuration
consistent for later reloads and reboots. If an existing file mount must be
updated in place, restrict any temporary writable transition to that exact file
and restore its read-only flag immediately. Never make the container root or
unrelated mounts writable. Preserve accepted streams throughout the repair.

## Rollback

Keep the previous validated proxy configuration and application artifact.
Rollback also changes only new requests; candidate requests already in progress
must drain naturally. Do not restore an older database, balance snapshot, key
set, or proxy ownership ledger as part of a traffic rollback. Validate billing
compatibility before reversing any pricing configuration.

DNS failover protects an entrance, not the shared application host, and cannot
migrate an established connection. Preserve ordinary-backup semantics, TLS
validation, record/target allowlists, and the independently protected direct
hostname when updating the probe to a new application.
