# Architecture

`buildinfodiff` is one standard-library Go file, `plugin/scripts/buildinfodiff.go`, plus a skill that tells Claude how to use it.

## Flow

1. **Read.** `Load` opens each input, or reads standard input for `-`. A file that starts with an ELF, PE or Mach-O magic number is an executable; anything else is text.
2. **Check executables.** `checkComplete` parses the headers with `debug/elf`, `debug/pe` or `debug/macho` and refuses the file when any segment or section ends past the end of the file. `debug/buildinfo` alone would read a cut-off binary whenever the build information comes before the cut. Universal Mach-O files are refused. Then `debug/buildinfo.Read` gets the build information.
3. **Parse text.** `ParseText` accepts `go version -m` output for one binary (a `name: goX` header and tab-indented lines), the text of `debug.BuildInfo.String()`, or `go version -m -json` output. It refuses empty, binary, UTF-16 and non-newline-terminated text, error messages from `go version`, other output, and more than one binary. The lines go through `debug.ParseBuildInfo`, so quoting matches the go command.
4. **Validate.** `validate` refuses build information without `-compiler`, `GOOS` or `GOARCH`, without the CPU level setting that the Go version writes for the architecture, with a VCS stamp missing `vcs.revision` or `vcs.modified`, with a bad time or checksum, or with a dependency listed twice. Go writes settings in a fixed order (flags, CGO, `GOARCH`, `GOOS`, CPU level, VCS), so text cut at a line break loses one of these.
5. **Check the end of text.** `checkTextEnds` handles the one cut the order can't show: text cut just before its VCS lines. Text from a local build (no checksum on the main module) without VCS lines, or text without VCS lines when the other side has them, exits 2 unless `--trust-no-vcs` is set.
6. **Compare.** `Compare` checks the program and platform, the toolchain, the VCS stamp, the module version, each dependency (version order, checksum, replacement) and each build setting. Changes that need a person become `Finding`s. The rest are listed for information.
7. **Output.** `WriteText` or JSON. Findings decide the `--fail` exit code.

## Why the checks are strict

A diff tool can report "nothing to worry about" for the wrong reason: one side is an error message, an empty file, or output cut off by a broken pipe or a failed copy. Every check in steps 2 to 5 exists so that a bad input never produces a report or exit code 0. The tests cut real text, JSON and binaries at many points to keep it that way.

## What it doesn't do

It doesn't build, download, pull images or run binaries. The skill tells Claude how to get the inputs, so the tool stays a pure function of its two files.
