# Roadmap

## Next
- A Markdown output for pull request comments and release notes
- An option to compare one binary against a commit (`--revision SHA`) and fail when it doesn't match
- Read build information from a container image layer tarball, so `docker save` output can be compared without running anything

## Later
- Group dependency changes by major version jumps and by modules that are new to the program
- Test on Windows and macOS hosts and in GitHub Actions
- Show which `DefaultGODEBUG` keys are documented as security relevant, from the Go release notes
