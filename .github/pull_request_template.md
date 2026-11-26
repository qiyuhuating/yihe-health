## What changed

<!-- Describe the user-visible or engineering behavior, not only the files changed. -->

## Why

<!-- State the problem and why this is the smallest safe solution. -->

## Verification

- [ ] I identified the changed behavior, affected version and any related issue.
- [ ] I ran or reviewed verification appropriate to the changed paths. For docs-only changes, checked links, commands and Issue template syntax.
- [ ] `Core CI / required` and `Security gate / required` passed where applicable. Any exceptional maintainer override is documented with the missing evidence; a skipped job is not described as passed.
- [ ] Any architecture or trust-boundary changes are reflected in `docs/ARCHITECTURE.md` or the relevant decision record.

### UI/UX changes

- [ ] Not applicable.
- [ ] UI Quality is green.
- [ ] Desktop, narrow/mobile, keyboard and large-text behavior were considered.
- [ ] Loading / empty / error / long-content / destructive states remain understandable.
- [ ] A visual baseline was changed only because the visual change is intentional.

### Data / security / desktop changes

- [ ] Not applicable.
- [ ] User data under `%APPDATA%\颐和` is preserved unless the user explicitly requests deletion; database and key migrations include a rollback or recovery plan.
- [ ] Tauri capabilities remain restricted, desktop tokens stay outside page JavaScript, and external navigation remains denied.
- [ ] Authentication / authorization / CSRF effects are covered by tests.
- [ ] Build/sign/release boundaries remain separated.

## Evidence and remaining limits

<!-- Link workflow runs, screenshots, logs or test output. List anything not verified. -->
