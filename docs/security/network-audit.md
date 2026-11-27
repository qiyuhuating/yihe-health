# Network and device audit boundaries

Security audit uses the network peer observed by the server, with forwarded addresses accepted only through the configured trusted proxy boundary. This is an observed connection address, not proof of the user's underlying address behind a VPN, NAT or relay.

Device type, operating system, browser, model and any existing application-scoped identifier supplied by the client are unverified hints. New audit entries distinguish three identifier states:

- `not-provided`: no identifier was supplied.
- `invalid-format`: a nonempty value failed the existing format validation; the invalid value is not retained.
- `app-reported`: the value passed format validation, but is not hardware attestation.

Historical entries may have an empty source. Treat that as unspecified, not evidence of abuse. Missing or malformed hints alone must not cause authorization or access restrictions. Audit strings are bounded and stripped of control and invisible formatting characters.

Deployers should explain security auditing and its retention in their privacy notice, restrict audit access to authorized personnel, and delete records according to the documented retention policy. Do not collect new hardware serial numbers, hostnames or network identifiers at launch. Use user-initiated network diagnostics for DNS troubleshooting; never bypass a VPN or proxy to obtain an underlying address. IP reputation can indicate a possible VPN, hosting provider or relay, not establish that an individual is malicious.

This change does not introduce a new collection endpoint, start-up telemetry, DNS probing, device fingerprinting or an authorization rule. Existing audit collection and retention are unchanged. It does not establish compliance or compare security effectiveness against proprietary third-party systems.

Reference: [Anthropic's public safeguards design](https://www.anthropic.com/news/building-safeguards-for-claude) describes correlating observed activity with external threat intelligence. Yihe uses its own deterministic controls and evidence requirements.
