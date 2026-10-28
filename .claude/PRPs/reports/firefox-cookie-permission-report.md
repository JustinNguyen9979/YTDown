# Implementation Report: Browser Profile Cookie Access

## Summary

Added browser-profile directory selection through the Wails API and persisted `selected_profile` in cookie configuration. Cookie extraction validates selected profile paths, constructs profile-specific yt-dlp specs, scopes browser and Xiaohongshu session caches to browser/profile, and returns actionable errors for inaccessible profiles or missing `web_session`. The XHS image direct-download and video yt-dlp routes remain in place. Browser cookie stores are closed after discovery and traversal; traversal uses a bounded context.

This follows the approved ad-hoc signing approach. It does not add security-scoped bookmarks, entitlements, or dependencies, and it does not modify `go.mod` or `go.sum` as part of this implementation. Those dependency files already contained unrelated user changes at task start and remain untouched by this task.

## Implementation Scope

| Area | Result |
|---|---|
| Wails profile picker and persisted selection | Implemented in `app.go` and generated bindings |
| Browser cookie-store profile validation | Firefox, Chromium-family, and Safari cookie-store paths implemented; unsupported adapters reject selection |
| yt-dlp browser profile arguments | Selected profile is passed as browser-specific profile path; supported syntax checked against installed yt-dlp help |
| XHS `web_session` extraction | In-memory browser/profile cache; missing session returns a recoverable error |
| Cookie access errors | Full Disk Access guidance is returned for permission-denied profile access; app does not change macOS settings |
| Store lifecycle | Discovery stores close after use; traversal owns store cleanup and uses a timeout context |
| UI | Profile selection and change controls use existing cookie UI; changing browser clears stale profile state |

## Validation Results

| Check | Result | Evidence |
|---|---|---|
| Focused browser-profile and XHS tests | PASS | `go test ./... -run 'Test(CookieStorePathFromProfile|BrowserProfileAccessError|CookieConfigPersistsSelectedProfile|HasSelectedBrowserProfile|GetCookieArgs|YTDLPBrowserSpec|FirefoxProfile|ExtractWebSessionFromNetscapeCookieFile|XHSBrowserCookie|XHSDownloadRoute|IsXiaohongshu)'` |
| Full Go tests | PASS | `go test ./...` |
| Static analysis | PASS | `go vet ./...` |
| Build | PASS | `go build ./...` |
| Race detection | PASS | `go test -race ./...` |
| Patch whitespace | PASS | `git diff --check` |
| Installed yt-dlp browser syntax | PASS | `yt-dlp --help` reports `BROWSER[+KEYRING][:PROFILE][::CONTAINER]` and lists Chrome, Chromium, Edge, Firefox, Brave, Opera, Safari, Vivaldi, and Whale |

Optional `staticcheck`, `golangci-lint`, and `govulncheck` were unavailable in the environment. The ECC Go reviewer could not access the primary checkout and refused to review an alternate worktree, so an ECC review of the actual diff remains outstanding. No macOS app bundle was available to verify TCC, Full Disk Access, or signing behavior at runtime.

## Tests Added or Extended

- `cookie_profile_test.go` covers Firefox/Chromium/Vivaldi/Safari cookie database paths, unsupported/missing stores, Full Disk Access guidance, browser profile spec paths, and browser/profile matching.
- `browser_detector_test.go` covers macOS browser bundle detection and browser ID normalization/profile arguments.
- `xhs_gallery_test.go` covers XHS image/video routing and fake Netscape cookie `web_session` extraction, including missing-session behavior.

The RED run for newly introduced XHS helper tests failed because the helpers were not implemented. After implementation, the focused test target passed. No checkpoint commits were created because the user explicitly requested that no commits be made.

## Known Limits and Follow-up

- The selected directory path is persisted, but macOS security-scoped bookmark access is not implemented. This is consistent with the approved ad-hoc signing/manual Full Disk Access approach, but actual packaged-app access still needs runtime verification.
- ECC Go review must be rerun in an environment that can access the primary checkout.
- Optional Go analyzers listed above were not installed.
- The plan remains at `FIREFOX_COOKIE_PERMISSION_PLAN.md`; it was not moved or archived.
