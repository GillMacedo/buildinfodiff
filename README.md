# buildinfodiff: compare two Go release binaries before you deploy

[![CI](https://github.com/GillMacedo/buildinfodiff/actions/workflows/ci.yml/badge.svg)](https://github.com/GillMacedo/buildinfodiff/actions/workflows/ci.yml) [![MIT license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

**A Claude Code skill and Go CLI for Go developers and SREs who ship binaries and want to know what really changed between the release running now and the one replacing it. It compares the build information Go embeds in every binary (`go version -m`) and lists dependency upgrades and downgrades, changed checksums, new replace directives, the Go toolchain, build tags, CGO, `-trimpath`, linker flags, `DefaultGODEBUG`, CPU level, and whether each binary was built from a clean, committed tree.**

A hotfix built on a laptop, a Docker build that lost `.git`, a `replace` left in `go.mod` from debugging, or a `go` line bump that silently changes TLS or HTTP defaults all pass code review, because none of them is in the diff of the code. They are all in the binary. `buildinfodiff` reads two binaries, or their saved `go version -m` output, and puts the changes that need a person first. It refuses an empty, garbled or cut-off input instead of calling it clean.

## What you can ask Claude

- "What changed between the binary in production and the new release?"
- "Was this hotfix binary built from committed code?"
- "Here are `go version -m` outputs from v1.4.1 and v1.5.0. Anything risky?"
- "The new build behaves differently but the code didn't change. Why?"
- "Did this release downgrade any dependency or add a replace directive?"

## Install

As a Claude Code plugin:

    claude plugin marketplace add GillMacedo/buildinfodiff
    claude plugin install buildinfodiff@buildinfodiff-marketplace

Or as a CLI with Go 1.22 or newer. It has no dependencies:

    go run github.com/GillMacedo/buildinfodiff/plugin/scripts@latest old-binary new-binary

## Example

`examples/orders` holds three versions of a small made-up service that uses two real modules, `github.com/google/uuid` and `golang.org/x/text`. `scripts/build-examples.sh` commits them to a throwaway git repository and builds them: v1.4.0 and v1.4.1 the way a release pipeline would (`CGO_ENABLED=0`, `-trimpath`, `-tags netgo`), and a hotfix the way a laptop would, from uncommitted changes, with a local fork of `uuid`, an older `x/text` and the `go` line raised from 1.22 to 1.25. It saves `go version -m` for each. From a clone of this repo:

    $ go run ./plugin/scripts examples/buildinfo/orders-v1.4.1.txt examples/buildinfo/orders-hotfix.txt
    base  examples/buildinfo/orders-v1.4.1.txt
          example.com/orders v1.4.1, go1.27.3, linux/arm64, 9b758fe1b7d6 2026-10-27
    head  examples/buildinfo/orders-hotfix.txt
          example.com/orders v1.4.1+dirty, go1.27.3, linux/arm64, 9b758fe1b7d6 2026-10-27 modified

    7 changes need a look:
      - Head was built from a working tree with uncommitted changes (vcs.modified=true), so revision 9b758fe1b7d6 doesn't identify its code.
      - github.com/google/uuid is replaced by the local directory ../uuid-fork in head, so its code isn't pinned by a version or checksum.
      - golang.org/x/text went down from v0.21.0 to v0.20.0.
      - Build tags changed: removed netgo.
      - -trimpath was removed, so head embeds the build machine's file paths.
      - DefaultGODEBUG changed, which changes runtime behavior without a code change: no longer set containermaxprocs=0, decoratemappings=0, gotestjsonbuildtext=1, httpcookiemaxnum=0, httpservecontentkeepheaders=1, multipathtcp=0, randseednop=0, rsa1024min=0, tlsmlkem=0, tlssha1=1, updatemaxprocs=0, urlmaxqueryparams=0, winreadlinkvolume=0, winsymlink=0, x509negativeserial=1, x509rsacrt=0, x509sha256skid=0, x509usepolicies=0.
      - CGO_ENABLED changed from 0 to 1. A Linux binary built with cgo usually needs the C library at run time, so it may fail in scratch or distroless images.

    Module version: v1.4.1 -> v1.4.1+dirty

    Dependencies: 1 downgraded, 1 now replaced, 1 added
      downgraded    golang.org/x/text  v0.21.0 -> v0.20.0
      now replaced  github.com/google/uuid  ../uuid-fork
      added         golang.org/x/sync  v0.10.0

    Other build settings:
      -ldflags: not recorded (Go leaves it out with -trimpath) -> -X main.version=v1.4.1-hotfix

v1.4.1 only changed comments, and was built the same way as v1.4.0, so it stays quiet:

    $ go run ./plugin/scripts examples/buildinfo/orders-v1.4.0.txt examples/buildinfo/orders-v1.4.1.txt
    base  examples/buildinfo/orders-v1.4.0.txt
          example.com/orders v1.4.0, go1.27.3, linux/arm64, 9cd8a81dc162 2026-10-20
    head  examples/buildinfo/orders-v1.4.1.txt
          example.com/orders v1.4.1, go1.27.3, linux/arm64, 9b758fe1b7d6 2026-10-27

    Nothing needs a look.

    Module version: v1.4.0 -> v1.4.1

With `--fail`, the first comparison exits 1 and the second exits 0.

### Real releases

These come from the official linux/arm64 release binaries of Kubernetes `kubectl` and Prometheus `node_exporter`, checked against their published SHA-256 sums.

Kubernetes 1.32 added `godebug winsymlink=0` to its `go.mod`, which shows up in the binary as a new `DefaultGODEBUG` value (first 12 lines):

    $ go run ./plugin/scripts examples/buildinfo/real/kubectl-1.31.4-linux-arm64.txt examples/buildinfo/real/kubectl-1.32.0-linux-arm64.txt | sed -n 1,12p
    base  examples/buildinfo/real/kubectl-1.31.4-linux-arm64.txt
          k8s.io/kubernetes (devel), go1.22.9, linux/arm64, a78aa47129b8 2024-12-10
    head  examples/buildinfo/real/kubectl-1.32.0-linux-arm64.txt
          k8s.io/kubernetes (devel), go1.23.3, linux/arm64, 70d3cc986aa8 2024-12-11

    1 change needs a look:
      - DefaultGODEBUG changed, which changes runtime behavior without a code change: newly set winsymlink=0.

    Go: go1.22.9 -> go1.23.3

    Dependencies: 22 upgraded, 2 added, 4 removed
      upgraded  github.com/distribution/reference  v0.5.0 -> v0.6.0

node_exporter 1.9.1 raised the `go` line in its `go.mod` from 1.21 to 1.23.0, so five old defaults that 1.8.2 had kept are gone, including `tls10server=1` and `httpmuxgo121=1`:

    $ go run ./plugin/scripts examples/buildinfo/real/node_exporter-1.8.2-linux-arm64.txt examples/buildinfo/real/node_exporter-1.9.1-linux-arm64.txt | sed -n 1,8p
    base  examples/buildinfo/real/node_exporter-1.8.2-linux-arm64.txt
          github.com/prometheus/node_exporter (devel), go1.22.5, linux/arm64, f1e0e8360aa6 2024-07-14
    head  examples/buildinfo/real/node_exporter-1.9.1-linux-arm64.txt
          github.com/prometheus/node_exporter (devel), go1.23.7, linux/arm64, f2ec547b49af 2025-04-01

    1 change needs a look:
      - DefaultGODEBUG changed, which changes runtime behavior without a code change: no longer set httplaxcontentlength=1, httpmuxgo121=1, tls10server=1, tlsrsakex=1, tlsunsafeekm=1.

The same two releases with the arguments swapped look like a rollback, and every downgrade is listed:

    $ go run ./plugin/scripts examples/buildinfo/real/node_exporter-1.9.1-linux-arm64.txt examples/buildinfo/real/node_exporter-1.8.2-linux-arm64.txt | sed -n 1,9p
    base  examples/buildinfo/real/node_exporter-1.9.1-linux-arm64.txt
          github.com/prometheus/node_exporter (devel), go1.23.7, linux/arm64, f2ec547b49af 2025-04-01
    head  examples/buildinfo/real/node_exporter-1.8.2-linux-arm64.txt
          github.com/prometheus/node_exporter (devel), go1.22.5, linux/arm64, f1e0e8360aa6 2024-07-14

    21 changes need a look:
      - Go toolchain went down from go1.23.7 to go1.22.5.
      - Head's commit (2024-07-14) is older than base's (2025-04-01). Check the argument order, or whether this is a rollback.
      - github.com/beevik/ntp went down from v1.4.3 to v1.3.1.

`go version -m -json` output of a binary reads the same as its text output:

    $ go run ./plugin/scripts examples/buildinfo/real/kubectl-1.32.0-linux-arm64.json examples/buildinfo/real/kubectl-1.32.0-linux-arm64.txt
    No differences: same Go version, module version, 82 dependencies and build settings.

### Input it won't trust

The output of a failed build is refused with exit code 2:

    $ go run ./plugin/scripts examples/buildinfo/orders-v1.4.1.txt tests/testdata/bad/go-build-failed.txt 2>&1 | sed -n 1,2p
    buildinfodiff: head can't be trusted, so nothing was compared.
      tests/testdata/bad/go-build-failed.txt: line 1 isn't `go version -m` output: "# command-line-arguments"

`go version -m` text cut off in the middle is refused too. Text cut off just before its last lines, the VCS stamp, looks exactly like a build without one, so it is refused unless you say the build really has no stamp:

    $ head -n 15 examples/buildinfo/orders-v1.4.1.txt | go run ./plugin/scripts examples/buildinfo/orders-v1.4.0.txt - 2>&1 | sed -n 1,2p
    buildinfodiff: head can't be trusted, so nothing was compared.
      standard input: has a vcs setting but no vcs.revision, so it is cut off

    $ head -n 14 examples/buildinfo/orders-v1.4.1.txt | go run ./plugin/scripts examples/buildinfo/orders-v1.4.0.txt - 2>&1 | sed -n 1,2p
    buildinfodiff: an input may be cut off, so nothing was compared.
      standard input: has no VCS lines, though it was built from a local directory. Either it was built without a VCS stamp (-buildvcs=false, or outside the repository), or the text was cut off just before its vcs lines, and the two look the same. Check the end of the file, or pass the binary itself. If the build really has no stamp, run again with --trust-no-vcs

A binary is safer than text: its headers say how long it should be, so a binary cut off by a failed copy is always refused.

## Usage

    buildinfodiff [--fail] [--json] [--trust-no-vcs] BASE HEAD

- `BASE` is the build you trust, usually the one running now, and `HEAD` replaces it. Each is a Go executable (ELF, Mach-O or PE), or the saved output of `go version -m FILE` or `go version -m -json FILE` for one binary. Use `-` for standard input.
- `--fail` exits 1 when a change needs a look.
- `--json` prints the full result, including both sides' dependencies and settings.
- `--trust-no-vcs` accepts `go version -m` text from a local build with no VCS lines, after you have checked it isn't cut off.
- Exit code 2 means bad usage or an input that can't be trusted. `go run` turns every non-zero exit code into 1, so build the tool with `go build` if you need to tell the two apart.

### Getting the inputs

- **Two binaries you have:** pass them directly.
- **A binary in a container image:** `docker create` the image and `docker cp` the binary out, or run `go version -m /path/to/binary` inside a container that has Go.
- **Two revisions of your repository:** build each with your real release command, in a separate `git worktree`, with the same environment. Otherwise the report shows how your two builds differed, not how the code did.

### In CI

    - run: go run github.com/GillMacedo/buildinfodiff/plugin/scripts@v0.1.0 --fail previous-release/app dist/app

You need a step that fetches the previous release's binary first. This hasn't been run in GitHub Actions yet.

## What it reports

| Finding | Meaning | Counts for `--fail` |
|---|---|---|
| Uncommitted changes | Head has `vcs.modified=true`, so its revision doesn't identify its code | yes |
| No VCS stamp | Base has a VCS stamp and head doesn't (`-buildvcs=false`, or built outside the repository) | yes |
| Downgrade | A dependency, a replacement, or the Go toolchain went down | yes |
| Checksum changed | The same module version has a different `h1:` hash | yes |
| Replace | A dependency is newly replaced, or its replacement changed. Local directories are called out | yes |
| DefaultGODEBUG | Runtime defaults changed, with each key that was added, removed or changed | yes |
| CGO, tags, `-trimpath` removed, other linker flags, `-race`, `-msan`, `-asan`, `-gcflags` with `-N` or `-l`, CPU level such as `GOAMD64`, `-buildmode` and other settings | A build setting differs | yes |
| Older commit or lower module version | Head's `vcs.time` or tagged version is older than base's | yes |
| Different program or platform | Main package path, `GOOS` or `GOARCH` differ | yes |
| Go upgrade, dependency upgrades, added and removed dependencies, replacements removed, `-X` values in `-ldflags`, `CGO_CFLAGS` and friends | Listed for information | no |

Go leaves `-ldflags` out of the build information when `-trimpath` is set, so a change there is shown as "not recorded" instead of compared.

## Limits

- **Metadata, not code.** It compares what Go records about the build. A code change in the main module, or a dependency whose code changed under a `replace` to a local directory, isn't visible beyond the replace itself.
- **Text can be cut off unnoticed in one place.** The checks catch text cut inside a line, before the build settings, or before the CPU level line. Text cut exactly before the VCS lines of a local build is indistinguishable from a build without a stamp, so it exits 2 unless you pass `--trust-no-vcs`. The CPU level check covers amd64, arm64, 386, arm, riscv64, mips, mips64 and ppc64; on other architectures a cut after `GOOS` isn't caught in text.
- **Go 1.18 or newer.** Older binaries have no build settings and are refused.
- **Pseudo-versions from the same second** can't be ordered, so they are reported as "changed".
- **Not tested** on Windows or macOS hosts, in GitHub Actions, in a live Claude Code session, through `go run ...@latest` (which needs the published repo), or on wasm, AIX or Plan 9 binaries, which it doesn't read; pass their `go version -m` output instead where Go supports it.
- `buildinfodiff` only reads files. It doesn't use the network or run anything. Claude sends what it reads to its model.

## FAQ

**How do I see what changed between two Go binaries?** Run `buildinfodiff old new`. It lists the Go version, dependency changes and build settings that differ, with risky ones first.

**How do I check whether a Go binary was built from a dirty tree?** Look for `vcs.modified=true` in `go version -m`. `buildinfodiff` reports it for head. For a single binary with no previous build to compare against, [buildstamp](https://github.com/go-fleettools/buildstamp) checks just that.

**How is this different from an SBOM diff?** SBOM tools such as `cyclonedx-cli` or `sbom-tools` compare package lists from any ecosystem. `buildinfodiff` reads Go's own build information directly, so it also compares build settings, `DefaultGODEBUG`, replace directives and VCS stamps, and needs no SBOM step.

**How is this different from go-modiff?** `go-modiff` compares `go.mod` between two git refs. `buildinfodiff` compares the binaries, which shows what was really linked and how it was built.

**Why does a new build behave differently when the code didn't change?** Common causes in the binary are a changed `DefaultGODEBUG` after a `go` line bump, a different Go toolchain, cgo turned on, or a different build tag. All four are reported.

**Is it an MCP server?** No. It's a skill with one Go source file, so `go run` is all it needs.

## Tested on

- **Real releases:** official linux/arm64 binaries of `kubectl` 1.31.4 and 1.32.0, and `node_exporter` 1.8.2, 1.9.1 and 1.10.2 (plus 1.9.1 linux/amd64), with checked SHA-256 sums. The `DefaultGODEBUG` changes shown above match the `go` and `godebug` lines in each project's `go.mod` at those tags. `stringer` v0.36.0 and v0.38.0 built with `go install`, which have no VCS stamp.
- **The example service**, built by `scripts/build-examples.sh` on Go 1.27.3, linux/arm64.
- **Binaries built in the tests:** the integration tests build a small program in a throwaway git repository with no network, and check release, dirty, `-buildvcs=false`, `-tags`, no `-trimpath`, `-gcflags`, `-race`, `GOAMD64` v1 and v3, and cross-compiled Windows and macOS binaries. Each binary must read the same as its own `go version -m` text output, and one of them as its JSON output, which Go 1.22 doesn't have.
- **Bad input** on each side, with and without `--fail`: empty and blank files, random bytes, a failed `go build`, a Go panic, `go version` without `-m`, `go version -m` on a wasm file and on a source file, `go list -m -json`, a 404 response, prose, two binaries in one output, UTF-16 text (converted with `iconv` the way PowerShell writes it) and an ELF file cut to 3000 bytes. A C program built with `cc` is refused as not Go. All exit 2.
- **Cut inputs:** the unit tests cut five pairs of real `go version -m` outputs at every line break and at 40 points inside lines, on each side, and JSON at every 37th byte. The integration tests cut binaries for linux/amd64, linux/arm64, windows/amd64 and darwin/arm64 at 64 points each. All of them exit 2.
- **Go versions:** the unit and integration tests pass on Go 1.27.3 and Go 1.22.12, both linux/arm64.

## Contributing

Contributions are welcome, from people and from people working with AI agents. See [CONTRIBUTING.md](CONTRIBUTING.md). Good first issues are [listed here](https://github.com/GillMacedo/buildinfodiff/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22).

### How pull requests work

1. **Open a PR.** Fork the repo, work on a branch, and keep it to one change. Write a title that says what changes for users, because it becomes the changelog line: `feat:` goes under Added and `fix:` under Fixed.
2. **Automatic check.** Code changes need tests, and `make test` must pass. Changes over 30 lines need a linked open issue (`Closes #<number>`), and a PR can have at most 800 changed lines. If something is missing, a comment tells you what to fix.
3. **Review.** Claude reviews each PR automatically. Code PRs also get a security review and are tested in a sandbox. The review then merges the PR or asks for changes. PRs that change CI, agent settings, npm or Python dependencies, vendored code or build files can't be merged this way, and a comment says so. The same goes for anything the security review doesn't clear.
4. **Release.** A merge that changes what users install becomes a new version within about two hours, with a GitHub Release and, if the project has one, its npm package. Docs and CI changes wait for the next release.

## AI disclosure

This project was largely generated with Claude Code and reviewed by its maintainer.

## About me

I'm Giulliano Macedo, and I build developer tools at GoTech Solutions. Follow along or get in touch:

- GitHub: [GillMacedo](https://github.com/GillMacedo)
- LinkedIn: [giulliano](https://www.linkedin.com/in/giulliano/)
- X: [@giumacedo](https://x.com/giumacedo)

If `buildinfodiff` saved you some prompts, a star helps others find it.

---

Built by [Giulliano Macedo](https://github.com/GillMacedo), GoTech Solutions · [LinkedIn](https://www.linkedin.com/in/giulliano/) · [X](https://x.com/giumacedo)
