# 0001: One standard-library Go file, shipped as a skill

**Decision:** all logic lives in `plugin/scripts/buildinfodiff.go`, which uses only the standard library. The plugin ships it with a skill, and Claude runs it with `go run`.

**Why:** the people who need it already have Go, and `debug/buildinfo`, `debug/elf`, `debug/macho` and `debug/pe` in the standard library do the reading. A single file needs no module download, no npm package and no MCP server. An agent can run it from a skill with one command, so an MCP server would add a process and an install step without giving the agent anything new.

**Cost:** the file is long, and tests import it as `package main`.
