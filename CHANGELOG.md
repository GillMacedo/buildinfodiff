# Changelog

All notable changes are documented here, in [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format. This project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-11-02
### Added
- Compares the build information of two Go binaries, read from ELF, Mach-O or PE executables, `go version -m` output or `go version -m -json` output
- Reports changes that need a look: uncommitted changes or a missing VCS stamp, dependency and toolchain downgrades, changed checksums, new or changed replace directives, changed DefaultGODEBUG, CGO, build tags, `-trimpath`, linker flags, `-race`, debug `-gcflags`, CPU level and other build settings, an older commit or module version, and a different program or platform
- Lists Go upgrades, dependency upgrades, additions and removals, and `-X` linker values for information
- Refuses empty, garbled, cut-off and multi-binary input with exit code 2, including binaries whose headers point past the end of the file
- `--fail`, `--json` and `--trust-no-vcs` options
- Claude Code plugin with the `comparing-go-release-binaries` skill
