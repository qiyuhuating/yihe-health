# Sensitive read admission and audit

High-cost authenticated read endpoints share a process-local admission budget,
keyed by the server-authenticated role and account ID, not by IP or a
client-supplied identifier. The initial fixed-window and concurrency limits
are implementation defaults, not evidence-based attack classification.
The limits are bounded and nonblocking. Requests above the budget return
HTTP 429 before the sensitive audit/database operation; active work is
released on handler return. This does not limit SOS or device ingestion.

The existing sensitive-access audit now covers all /api/v1/health/ routes
for authenticated principals. The existing durable audit-before-handler
contract is unchanged. It records attempts rather than proving that the
response disclosed data. The admission-denied requests are not individually
persisted by this middleware. No credentials, raw session tokens, request
bodies or new device identifiers are collected.

Existing authorization and pagination/range constraints remain authoritative.
The budget is per process, resets on process restart, and does not implement
distributed data-volume quotas, export policy, outcome-based alerting,
automatic session revocation or network bans. In particular, repeated 429
responses alone do not establish malicious intent. Validate limits against
normal caregiver workflows before production rollout.
