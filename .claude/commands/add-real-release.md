Add a real-release example for: $ARGUMENTS

1. Download two official release binaries of the open-source Go project for one platform, check them against the project's published checksums, and check that its license allows including its build information.
2. Save `go version -m` for each as `examples/buildinfo/real/<project>-<version>-<os>-<arch>.txt`, with the file name in the first line replaced by the program name.
3. Run `go run ./plugin/scripts` on the pair, and check every finding against the project's changelog and `go.mod` at both tags. Note the versions, platform, checksums and license in `examples/README.md`.
4. Add the pair to `TestRealReleases` and `TestCutTextIsNeverTrusted` in `plugin/scripts/buildinfodiff_test.go`, then run `make test`.
