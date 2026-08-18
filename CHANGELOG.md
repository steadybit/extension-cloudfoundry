# Changelog

## v1.0.6

- chore(deps): bump steadybit kits and drop Go patch pin (#23)
- chore(deps): bump steadybit kits and drop Go patch pin (#24)

## v1.0.5

- Add a "Fail early" option to the app state check. When enabled (the default, matching the previous behavior), the check fails as soon as a deviating state is observed. When disabled, the check keeps collecting events for the whole duration and only fails at the end of the step (with a past-tense message, since the state may have recovered by then).
- chore(deps): bump github.com/steadybit/action-kit/go/action_kit_sdk
- chore(deps): bump github.com/steadybit/discovery-kit/go/discovery_kit_sdk
- chore(deps): bump github.com/steadybit/extension-kit
- chore(deps): bump go to 1.26.5 (#20)
- chore(deps): update dependencies
- chore: add Claude Code workflows (#15)
- chore: silence SonarQube finding on secrets: inherit in Claude workflows
- feat(app state check): add fail early option (#16)
- feat: support filtering targets out of discovery
- fix: emit the app state metric immediately on Start (#22)
- refactor: register extension index via exthttp.RegisterRevisionedHandler (#21)

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