# Committed resident-location access audit

The single-resident location query persists an additional logical resource
in the existing MySQL `care_sensitive_access_audit` table:

    resident-location:committed:<resident ID>

The actor ID and role come from the authorized session. The numeric target
is the validated resident scope of the query. The audit insert occurs in
the same transaction as the authorized location read, after successful
result construction and before commit. A failed insert or commit prevents
the location response. The existing middleware's request-attempt audit
is retained; the additional record denotes a committed authorized query,
even if it returned no observations, not proof of client receipt.

This reuses the existing authorized audit viewer and schema. It intentionally
does not save coordinates, names, request bodies, raw session tokens or new
device metadata. A denied request does not create a committed-access record.
The record is not a separate failure or rollback audit and is not a claim
that all resident-read endpoints have target-level audit coverage.

The extra insert extends the authorized transaction by one statement. Measure
the cost under real care workloads before extending this pattern to bulk
reads. Existing retention and permission mechanisms are unchanged; deployers
should disclose the security audit purpose and restrict access to authorized
reviewers. No new migration or database dependency is required.
