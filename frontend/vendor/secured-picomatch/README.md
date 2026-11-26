# Secured picomatch

This development-only package replaces picomatch 4.0.7 for CVE-2026-112366. It comes from the MIT-licensed `@ryotsu/picomatch@4.0.7-a` release, with source at [3ab5a762](https://github.com/ryotsu-kankichi/secured-picomatch/tree/3ab5a7627d480259e3d9ca67fbc14c125d374f0e). The registry signature and release attestation were verified. The license and remaining JavaScript files match that source; the local changes below are maintained here. The local manifest excludes the fork's test and release tooling.

Local hardening: `lib/utils.js` replaces `removeBackslashes`' unanchored bracket regex with a linear scan and cached bracket ends, and `lib/constants.js` removes the unused expression. It preserves bracket-class escapes, trailing backslashes and all four JavaScript line terminators. Regression tests cover these semantics and a bounded child process with 250,000 unmatched brackets. This addresses a separate CodeQL polynomial-ReDoS finding; it does not claim a new upstream release or CVE assignment.

The npm lockfile links to this checked-in package. GitHub's dependency review associates the registry alias with the original vulnerable package. The existing dependency review and high-severity npm audit still apply. Remove this directory and the override when the official package publishes a verified fix.
