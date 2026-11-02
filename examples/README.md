# Examples

## `orders/`
A small made-up HTTP service in three versions. Each is its own Go module using `github.com/google/uuid` v1.6.0 (BSD-3-Clause) and `golang.org/x/text` (BSD-3-Clause). `before` is release v1.4.0. `comments` only adds two comments and is tagged v1.4.1. `hotfix` adds `golang.org/x/sync` v0.10.0 (BSD-3-Clause), downgrades `golang.org/x/text` from v0.21.0 to v0.20.0, replaces `github.com/google/uuid` with a local copy in `../uuid-fork`, and raises the `go` line from 1.22 to 1.25.

## `buildinfo/`
All real output of `go version -m`, never edited except that the file path in the first line is replaced with the program name.

- `orders-v1.4.0.txt`, `orders-v1.4.1.txt`, `orders-hotfix.txt`: from `scripts/build-examples.sh` (`make examples`), on Go 1.27.3, linux/arm64. The script commits each version to a throwaway git repository with fixed dates. The two releases are built with `CGO_ENABLED=0 go build -trimpath -tags netgo -ldflags "-s -w -X main.version=..."` from a tagged, clean tree. The hotfix is built with plain `go build -ldflags "-X main.version=v1.4.1-hotfix"` from uncommitted changes on top of v1.4.1, with cgo on because a C compiler is installed, and with an unmodified copy of `github.com/google/uuid` v1.6.0 from the module cache as `../uuid-fork`.
- `real/kubectl-1.31.4-linux-arm64.*` and `real/kubectl-1.32.0-linux-arm64.*`: from the official `kubectl` binaries at `dl.k8s.io` (Apache-2.0), checked against the published `.sha256` files. `.txt` is `go version -m` and `.json` is `go version -m -json`, both from Go 1.27.3.
- `real/node_exporter-*-linux-arm64.txt` and `real/node_exporter-1.9.1-linux-amd64.txt`: from the official node_exporter release tarballs on GitHub (Apache-2.0), checked against the release's `sha256sums.txt`.
- `real/stringer-v0.36.0-go-install.txt`, `real/stringer-v0.38.0-go-install.txt`: `go install golang.org/x/tools/cmd/stringer@VERSION` (BSD-3-Clause) on Go 1.27.3. Builds from the module proxy have no VCS stamp.

## `../tests/testdata/bad/`
Output of failed or wrong commands, used to check that `buildinfodiff` refuses them: a failed `go build`, a Go panic from `go run`, `go version` without `-m`, `go version -m` on a wasm file (an error) and on a source file (no output), `go list -m -json`, a 404 body saved by `curl`, `go version -m` on a directory with two binaries, the first 3000 bytes of a Go ELF binary, empty and blank files, random bytes and prose. `utf16-powershell.txt` is real `go version -m` output converted to UTF-16 with `iconv`, the encoding PowerShell's `>` writes; it wasn't made with PowerShell. Paths under `/tmp` were replaced with `/home/user`.
