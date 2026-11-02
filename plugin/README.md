# buildinfodiff plugin

This plugin adds the `comparing-go-release-binaries` skill to Claude Code. When you ask what changed between two Go releases or builds, Claude gets both binaries or their `go version -m` output and runs the bundled `scripts/buildinfodiff.go` with `go run`. It lists changes that need a look, such as a dirty working tree, downgrades, changed checksums, local replace directives, and changed CGO, tags, ldflags, DefaultGODEBUG or CPU level, then the dependency and toolchain changes. It needs Go 1.22 or newer. See the repository README for examples and limits.

Install it with `claude plugin marketplace add GillMacedo/buildinfodiff` and then `claude plugin install buildinfodiff@buildinfodiff-marketplace`.
