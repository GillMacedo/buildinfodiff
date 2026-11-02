# Security

Report vulnerabilities through [GitHub's private vulnerability reporting](https://github.com/GillMacedo/buildinfodiff/security/advisories/new), not in a public issue.

`buildinfodiff` reads two files: Go executables or saved `go version -m` output. It never writes files, uses the network or runs other programs. Please report privately if a crafted binary or text file could make it do anything else, or hang or use unbounded memory. Also report privately a case where it calls a dirty, replaced or tampered build clean, because that could let a bad binary ship. Other wrong or missed findings are normal bugs.
