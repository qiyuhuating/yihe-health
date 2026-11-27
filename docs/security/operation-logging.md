# Server-side operation logs

The HTTP middleware emits one structured completion record for each handled
HTTP request, with a server-generated request ID, bounded route, method,
HTTP status, elapsed time and response bytes accepted by the response writer.
Protected API requests additionally carry the server-validated account ID
and role when available, including role-denied requests. Requests rejected
before authentication, public endpoints and device-only requests may have
no attributed account. Login outcomes remain in the existing login audit.

The HTTP status is not a claim that a business operation committed, nor is
the byte count a count of records disclosed or proof that the client received
them. Existing transactional and sensitive-access audits remain authoritative.
No request or response bodies, query arguments, raw cookies or tokens,
additional device identifiers or client-side interaction tracking are added.

Deployments should disclose operational logging in the privacy notice,
restrict access to authorized operators, configure log retention and rotation,
and monitor collector failures. The JSON stream uses the existing logging
sink; these records are not a new durable MySQL audit table and cannot be
guaranteed after an abrupt process termination or sink failure. The change
does not send a separate client request or perform a second session lookup.
