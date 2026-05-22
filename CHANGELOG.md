# Changelog

## v0.1.6

- Stream selected HAR exports without rebuilding the full subset response buffer
- Bound entry detail body previews before full text materialization and preserve truncation semantics for large, wrapped, and malformed bodies
- Align release setup and README API/runtime documentation with the current workflow and server behavior

## v0.1.5

- Use curated `CHANGELOG.md` sections for GitHub Release notes
- Document the version update workflow to keep `VERSION` and release notes in sync

## v0.1.4

- Pin privileged GitHub Actions workflows to immutable commit SHAs
- Limit concurrent upload memory reservation before reading request bodies
- Align public release automation, export checks, and repository ruleset guidance
- Refresh public README screenshots and layout polish
