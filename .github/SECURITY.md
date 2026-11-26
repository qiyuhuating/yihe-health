# Security Policy

## Supported versions and branches

| Source | Scope of security maintenance |
| --- | --- |
| `main` and the latest official `v2.1.0+20261122` release | Current stable source; reports are accepted and fixes are assessed for an updated release |
| `preview` | Active development; report vulnerabilities in the current HEAD |
| Older tags and archived packages | Retained for reproducibility, without a promise of backports; report an issue privately if it also affects a supported source |

A patch merged into `preview` does not automatically patch previously downloaded installers. Check each release's exact Git SHA and assets.

## Reporting a vulnerability

Do not open a public issue containing an exploitable vulnerability, real user records, credentials, database files, private keys, SMS credentials, device secrets or personally identifying health data.

Use the repository's [private vulnerability reporting form](https://github.com/WatchTestCs/smart-elderly-watch-management/security/advisories/new) to report security issues without disclosing them in a public Issue or PR. Include only the evidence necessary to reproduce the problem. Do not attach actual residents' medical records, addresses or access credentials. If the private form is temporarily unavailable, contact a repository maintainer privately.

A useful report includes:

- affected source commit, release tag, or development branch;
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
