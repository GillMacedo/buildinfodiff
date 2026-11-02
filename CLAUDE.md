# buildinfodiff

Claude Code skill and Go CLI that compares the build information Go embeds in two binaries, such as the release running now and the one replacing it, and reports dependency, toolchain, build setting and VCS changes, with the ones that need a person first.

## Architecture
`plugin/scripts/buildinfodiff.go` holds all the logic in one standard-library file, so `go run` needs no setup. `Load` reads a binary (after `checkComplete` refuses a cut-off ELF, Mach-O or PE file) or text, `ParseText` reads `go version -m` text and JSON, `validate` refuses incomplete build information, `checkTextEnds` refuses text that may be cut off before its VCS lines, and `Compare` builds the `Report`. `plugin/skills/comparing-go-release-binaries/SKILL.md` tells Claude how to get two inputs and explain the report. See `docs/architecture.md`.

## Commands
- `make test`: gofmt, vet, unit tests, and integration tests in `tests/`, which build real binaries offline and run every `$ ` line in the README and check its output
- `make lint`: plugin and marketplace validation
- `make demo`: compare the example builds
- `make examples`: rebuild `examples/buildinfo/orders-*.txt` from `examples/orders` (needs the network the first time)

## Conventions
- Standard library only. Never use the network, write files or run programs from the tool.
- Input that isn't complete build information for one binary exits 2 with a message. Never report "No differences" or exit 0 for it. Add every new kind of bad input to `tests/testdata/bad/`.
- Every new finding needs a real build in `examples/buildinfo/` or in `tests/integration_test.go` that it fires on, and must stay quiet on `orders-v1.4.0.txt` against `orders-v1.4.1.txt`.
- Files in `examples/buildinfo/` are real output. `examples/README.md` says what made each one.
- README examples are generated from real output. If output changes, update the README, or `TestReadmeExamples` fails.

## Do and don't
- Do say a finding comes from build metadata, not from the code.
- Don't say two builds are the same because the report is short.

## Definition of done
`make test` and `make lint` pass, and `ROADMAP.md`, the README and docs are updated when relevant.
