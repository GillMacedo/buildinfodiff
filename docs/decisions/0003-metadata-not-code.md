# 0003: Compare build metadata, not code

**Decision:** `buildinfodiff` compares only what Go records in the binary. It doesn't disassemble, diff symbols or read source.

**Why:** the build information answers the questions that code review misses (how and from what the binary was built) exactly and cheaply. Code-level binary diffs exist and are a different job.

**Cost:** a code change in the main module, or under a local `replace`, shows up only as a revision or a replace finding. The README and the skill say so.
