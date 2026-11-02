# 0002: Refuse input that may be incomplete

**Decision:** any input that isn't clearly complete build information for one binary exits 2 and isn't compared. That includes binaries whose headers point past the end of the file, and `go version -m` text from a local build that has no VCS lines, unless `--trust-no-vcs` is set.

**Why:** earlier tools in this family passed their own tests and still reported a failed build or an unreadable report as all clear. Here, a head side cut off before its VCS lines would hide a dirty build, and a cut-off binary can still hold readable build information.

**Cost:** a local build really made with `-buildvcs=false` needs `--trust-no-vcs` when passed as text. Passing the binary avoids it.

**Alternatives:** trusting anything `debug.ParseBuildInfo` accepts. It accepts every prefix of valid output that ends at a line break, so most cuts would have been compared.
