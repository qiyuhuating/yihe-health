# Security Policy

## Supported branches

| Source | Scope of security maintenance |
| --- | --- |
| `main` | Active development branch (no pre-built releases yet); report vulnerabilities in the current HEAD |

## Reporting a vulnerability

Do not open a public issue containing an exploitable vulnerability, real user records, credentials, database files, private keys, SMS credentials, device secrets or personally identifying health data.

Use the repository's [private vulnerability reporting form](https://github.com/qiyuhuating/yihe-health/security/advisories/new) to report security issues without disclosing them in a public Issue or PR. Include only the evidence necessary to reproduce the problem. Do not attach actual residents' medical records, addresses or access credentials. If the private form is temporarily unavailable, contact a repository maintainer privately.

A useful report includes:

- affected source commit or branch;
- affected component (WebView, Tauri/Rust Main, Go service, installer, device API, etc.);
- prerequisites and realistic impact;
- minimal reproduction steps;
- whether the issue crosses a documented trust boundary;
- suggested mitigation if known.

## Security boundaries that must not be weakened silently

- the WebView has no Node.js or unrestricted shell/file access.
- remote IPC capabilities are limited to the main loopback window; native initialization also validates the exact origin.
- external navigation is rejected; third-party scripts and frames are blocked by the service CSP.
- the desktop-launched Go service binds only to loopback; standalone API deployments must explicitly restrict ADDR.
- the desktop bootstrap token is not exposed to Renderer JavaScript or localStorage.
- business authorization remains server-side; the desktop token is not a substitute for user authorization.
- user data is not stored in the installation directory.
- ordinary PR/CI jobs do not receive signing private keys.

See `docs/ARCHITECTURE.md` for the full invariants.

## Scope notes

The project has automated backend, UI/a11y, build-contract and static-security checks, but these are not a claim of complete security. Real-watch integration, real SMS, formal performance testing and a complete independent penetration test remain outside the currently published evidence.
