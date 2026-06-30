# Changelog

## Unreleased

- Add a "Fail early" option to the app state check. When enabled (the default, matching the previous behavior), the check fails as soon as a deviating state is observed. When disabled, the check keeps collecting events for the whole duration and only fails at the end of the step (with a past-tense message, since the state may have recovered by then).

## v1.0.4

- chore(deps): bump github.com/steadybit/extension-kit
- chore(deps): bump golang.org/x/net to v0.55.0 (CVE-2026-39821) (#11)

## v1.0.3

- chore(deps): bump alpine from 3.23 to 3.24

## v1.0.2

- chore(deps): bump github.com/steadybit/discovery-kit/go/discovery_kit_sdk
- chore: update to go 1.26.4
- feat: add weekly auto patch-release workflow

## v1.0.1

 - Update to Go 1.26.3 and bump dependencies

## v1.0.0

 - Initial release