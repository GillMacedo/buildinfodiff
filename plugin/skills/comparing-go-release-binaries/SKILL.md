---
name: comparing-go-release-binaries
description: Compares the build information Go embeds in two binaries, such as the release running in production and the one about to replace it, and reports dependency upgrades and downgrades, changed checksums, new replace directives, the Go toolchain, build tags, CGO, -trimpath, ldflags, DefaultGODEBUG, CPU level, and whether each was built from a clean, committed tree. Use when the user asks what changed between two Go releases or builds, whether a release binary was built from a dirty tree or a local fork, why a new build behaves differently with the same code, what a hotfix build contains, or shares two `go version -m` outputs.
---

# Comparing Go release binaries

Every Go binary built with modules carries its own build information: the Go version, every linked module with its version and checksum, replace directives, build flags and the VCS commit. Two builds of the "same" code can differ in ways code review never sees. The bundled tool compares two of them and lists what changed, with the changes that need a person first. It needs Go 1.22 or newer. It reads files and never uses the network.

## Get the two inputs

The tool reads Go executables (ELF, Mach-O, PE) directly, or saved output of `go version -m FILE` or `go version -m -json FILE`.

1. **Base** is the build you trust, usually the one running now. **Head** replaces it.
2. If the user has both binaries, use them. A binary is better than text, because a cut-off binary is always caught.
3. If only text is available, for example from a container or a CI log, save the whole output of `go version -m /path/to/binary` for one binary per file.
4. To compare two revisions of the repository, build each with the project's own release command (look in the `Makefile`, `.goreleaser.yaml` or CI workflow), in a separate worktree, for example `git worktree add /tmp/base v1.4.0`. Use the same command and environment for both, or the report shows your build differences instead of the code's.
5. Ask before pulling images, copying files out of running containers or clusters, or downloading release assets.

## Run the tool

    go run "${CLAUDE_PLUGIN_ROOT}/scripts/buildinfodiff.go" BASE HEAD

- `--fail` exits 1 when a change needs a look, for CI.
- `--json` gives the full result, including every dependency and setting of each side.
- Exit code 2 means an input can't be trusted: empty, not build information, cut off, or more than one binary in one file. Report the message and get a complete input. Never present the result of a run that ended with exit code 2 as a comparison.
- `--trust-no-vcs` accepts `go version -m` text from a local build that has no VCS lines. Use it only after checking with the user that the build really had no VCS stamp, for example because it used `-buildvcs=false`. Prefer passing the binary.

## Explain each finding

For each item under "needs a look", say what it means for this project and what to check:

1. **Uncommitted changes (vcs.modified=true) or no VCS stamp:** the commit doesn't identify the code. Ask where the binary was built and whether it should go out.
2. **Downgrades, changed checksums, new replace directives:** look at `go.mod` and `go.sum` at head's commit. A local directory replacement means code that no version pins. A changed checksum for the same version should never happen with the public module proxy, so treat it as a supply chain question.
3. **DefaultGODEBUG:** runtime behavior changed without a code change, usually because the `go` line in `go.mod` moved. Look each key up at https://go.dev/doc/godebug and say which ones this program could hit.
4. **CGO_ENABLED, tags, -trimpath, other linker flags, -race, -gcflags, CPU level:** compare with the project's release command and ask whether the change was intended. A cgo binary on Linux usually needs libc at run time.
5. **Toolchain downgrade, older commit, lower module version:** this may be a rollback or the arguments in the wrong order.

Then summarize the informational part: the Go toolchain change, the number of upgraded, added and removed dependencies, and any `-X` version stamps.

## Report

1. Lead with the findings that need a look, each with what to check.
2. Then the dependency and toolchain changes, grouped. Point out major version jumps and dependencies that are new to the program.
3. Say what the comparison can't show: it compares build metadata, not code, so a code change inside the same module version, or in the main module itself, doesn't appear.

## Don't

- Don't say two builds are the same because the report is short. Changes in the main module's own code don't show up here.
- Don't paste the full `go version -m` output into the conversation when the report is enough.
- Don't edit `go.mod`, rebuild or redeploy unless the user asks.
