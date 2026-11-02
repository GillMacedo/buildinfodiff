// Command buildinfodiff compares the build information that Go embeds in two
// binaries, such as the release running in production and the one about to
// replace it. It reports dependency changes, the Go toolchain, build settings
// and VCS stamps, and flags the changes that need a person to look: a dirty
// working tree, a missing VCS stamp, downgrades, changed checksums, new
// replace directives, and changed CGO, tags, ldflags, GODEBUG defaults or CPU
// level.
//
// It reads Go executables (ELF, Mach-O, PE), `go version -m` output, and
// `go version -m -json` output. Input it can't fully trust exits 2.
//
// It uses only the standard library, so `go run buildinfodiff.go` needs no
// setup. It never uses the network, writes files or runs other programs.
package main

import (
	"bytes"
	"debug/buildinfo"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const version = "0.1.0"

// maxText is the largest text or JSON input read. Real `go version -m` output
// is a few kilobytes; Kubernetes is under 20 KB.
const maxText = 16 << 20

// Module is one module in the build: the main module or a dependency.
type Module struct {
	Path    string  `json:"path"`
	Version string  `json:"version"`
	Sum     string  `json:"sum,omitempty"`
	Replace *Module `json:"replace,omitempty"`
}

// Setting is one build setting, such as GOOS=linux or -tags=netgo.
type Setting struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Info is the build information of one binary.
type Info struct {
	Name      string    `json:"name"`
	Format    string    `json:"format"`
	GoVersion string    `json:"goVersion"`
	Path      string    `json:"path"`
	Main      Module    `json:"main"`
	Deps      []Module  `json:"deps"`
	Settings  []Setting `json:"settings"`
}

// Get returns a build setting.
func (in *Info) Get(key string) (string, bool) {
	for _, s := range in.Settings {
		if s.Key == key {
			return s.Value, true
		}
	}
	return "", false
}

// HasVCS reports whether the binary carries a VCS stamp.
func (in *Info) HasVCS() bool {
	_, ok := in.Get("vcs")
	return ok
}

// InputError means an input can't be trusted, so nothing is compared.
type InputError struct {
	Name string
	Msg  string
}

func (e *InputError) Error() string { return e.Name + ": " + e.Msg }

func bad(name, format string, args ...any) error {
	return &InputError{Name: name, Msg: fmt.Sprintf(format, args...)}
}

// Load reads one input file, or standard input for "-".
func Load(name string) (*Info, error) {
	if name == "-" {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<30))
		if err != nil {
			return nil, bad("standard input", "can't read: %v", err)
		}
		return LoadBytes("standard input", data)
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, bad(name, "can't open: %v", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, bad(name, "can't read: %v", err)
	}
	if st.IsDir() {
		return nil, bad(name, "is a directory. Pass one binary or one saved `go version -m` output")
	}
	head := make([]byte, 8)
	n, _ := io.ReadFull(f, head)
	if isExecutable(head[:n]) {
		return readExecutable(name, f, st.Size())
	}
	if st.Size() > maxText {
		return nil, bad(name, "is %d bytes and isn't an executable this tool reads (ELF, Mach-O or PE)", st.Size())
	}
	data, err := io.ReadAll(io.NewSectionReader(f, 0, st.Size()))
	if err != nil {
		return nil, bad(name, "can't read: %v", err)
	}
	return LoadBytes(name, data)
}

// LoadBytes reads an input that is already in memory.
func LoadBytes(name string, data []byte) (*Info, error) {
	if isExecutable(data) {
		return readExecutable(name, bytes.NewReader(data), int64(len(data)))
	}
	return ParseText(name, data)
}

var (
	elfMagic   = []byte("\x7fELF")
	machoMagic = [][]byte{{0xfe, 0xed, 0xfa, 0xce}, {0xfe, 0xed, 0xfa, 0xcf}, {0xce, 0xfa, 0xed, 0xfe}, {0xcf, 0xfa, 0xed, 0xfe}}
	fatMagic   = [][]byte{{0xca, 0xfe, 0xba, 0xbe}, {0xbe, 0xba, 0xfe, 0xca}}
)

func isExecutable(b []byte) bool {
	if bytes.HasPrefix(b, elfMagic) || bytes.HasPrefix(b, []byte("MZ")) {
		return true
	}
	for _, m := range append(machoMagic, fatMagic...) {
		if bytes.HasPrefix(b, m) {
			return true
		}
	}
	return false
}

// readExecutable checks that the file is a complete executable, then reads
// its build information with debug/buildinfo.
func readExecutable(name string, r io.ReaderAt, size int64) (*Info, error) {
	if err := checkComplete(r, size); err != nil {
		return nil, bad(name, "%v", err)
	}
	bi, err := buildinfo.Read(r)
	if err != nil {
		return nil, bad(name, "has no Go build information (%v). It isn't a Go binary, or was built without module support", err)
	}
	in := fromBuildInfo(bi)
	in.Name, in.Format = name, "binary"
	if err := validate(in); err != nil {
		return nil, err
	}
	return in, nil
}

// checkComplete refuses an executable whose headers point past the end of
// the file, which is how a binary cut off by a failed copy or download looks.
// debug/buildinfo alone can read a cut-off file if the build information
// happens to come before the cut.
func checkComplete(r io.ReaderAt, size int64) error {
	head := make([]byte, 4)
	if _, err := r.ReadAt(head, 0); err != nil {
		return fmt.Errorf("is too short to be an executable")
	}
	cut := func(what string, end uint64) error {
		return fmt.Errorf("is cut off: its %s ends at byte %d, but the file has %d bytes. Copy or download it again", what, end, size)
	}
	switch {
	case bytes.HasPrefix(head, elfMagic):
		f, err := elf.NewFile(r)
		if err != nil {
			return fmt.Errorf("is an ELF file that can't be read, so it may be cut off or damaged: %v", err)
		}
		for _, p := range f.Progs {
			if end := p.Off + p.Filesz; end > uint64(size) {
				return cut("program segment", end)
			}
		}
		for _, s := range f.Sections {
			if s.Type == elf.SHT_NOBITS || s.Type == elf.SHT_NULL {
				continue
			}
			if end := s.Offset + s.FileSize; end > uint64(size) {
				return cut("section "+s.Name, end)
			}
		}
	case bytes.HasPrefix(head, []byte("MZ")):
		f, err := pe.NewFile(r)
		if err != nil {
			return fmt.Errorf("is a PE file that can't be read, so it may be cut off or damaged: %v", err)
		}
		for _, s := range f.Sections {
			if end := uint64(s.Offset) + uint64(s.Size); s.Size > 0 && end > uint64(size) {
				return cut("section "+s.Name, end)
			}
		}
	default:
		for _, m := range fatMagic {
			if bytes.Equal(head, m) {
				return fmt.Errorf("is a universal (fat) Mach-O file. Extract one architecture with `lipo -thin`, or pass `go version -m` output")
			}
		}
		f, err := macho.NewFile(r)
		if err != nil {
			return fmt.Errorf("is a Mach-O file that can't be read, so it may be cut off or damaged: %v", err)
		}
		for _, l := range f.Loads {
			if s, ok := l.(*macho.Segment); ok {
				if end := s.Offset + s.Filesz; end > uint64(size) {
					return cut("segment "+s.Name, end)
				}
			}
		}
		for _, s := range f.Sections {
			if s.Offset == 0 {
				continue
			}
			if end := uint64(s.Offset) + s.Size; end > uint64(size) {
				return cut("section "+s.Name, end)
			}
		}
	}
	return nil
}

func fromBuildInfo(bi *debug.BuildInfo) *Info {
	conv := func(m *debug.Module) Module {
		out := Module{Path: m.Path, Version: m.Version, Sum: m.Sum}
		if m.Replace != nil {
			out.Replace = &Module{Path: m.Replace.Path, Version: m.Replace.Version, Sum: m.Replace.Sum}
		}
		return out
	}
	in := &Info{GoVersion: bi.GoVersion, Path: bi.Path, Main: conv(&bi.Main)}
	for _, d := range bi.Deps {
		in.Deps = append(in.Deps, conv(d))
	}
	for _, s := range bi.Settings {
		in.Settings = append(in.Settings, Setting{Key: s.Key, Value: s.Value})
	}
	return in
}

// ParseText reads `go version -m` output for one binary, `go version -m
// -json` output, or the text of debug.BuildInfo.String().
func ParseText(name string, data []byte) (*Info, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, bad(name, "is empty")
	}
	if bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff}) {
		return nil, bad(name, "is UTF-16 text, which PowerShell's > writes. Save it with `| Out-File -Encoding utf8`, or pass the binary itself")
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return nil, bad(name, "is binary data, but not an executable this tool reads (ELF, Mach-O or PE)")
	}
	trimmed := bytes.TrimSpace(data)
	if trimmed[0] == '{' {
		return parseJSON(name, trimmed)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		return nil, bad(name, "doesn't end with a newline, so it may be cut off. Save the whole output of `go version -m`")
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	first := 0
	for first < len(lines) && strings.TrimSpace(lines[first]) == "" {
		first++
	}
	header := strings.TrimRight(lines[first], "\r")
	var goVersion string
	indent := ""
	switch {
	case strings.HasPrefix(header, "go\t"):
		goVersion = strings.TrimPrefix(header, "go\t")
	case strings.Contains(header, ": go") || strings.Contains(header, ": devel "):
		i := strings.LastIndex(header, ": go")
		if j := strings.LastIndex(header, ": devel "); j > i {
			i = j
		}
		goVersion = header[i+2:]
		indent = "\t"
	case strings.Contains(header, "could not read Go build info") || strings.Contains(header, "not a Go executable"):
		return nil, bad(name, "holds an error from `go version -m`, not build information: %q", clip(header))
	default:
		return nil, bad(name, "line %d isn't `go version -m` output: %q", first+1, clip(header))
	}
	var b strings.Builder
	b.WriteString("go\t" + goVersion + "\n")
	for i := first + 1; i < len(lines); i++ {
		l := strings.TrimRight(lines[i], "\r")
		if strings.TrimSpace(l) == "" {
			continue // go version -m prints a line holding one tab after a replaced dependency
		}
		if indent != "" {
			if !strings.HasPrefix(l, indent) {
				if strings.Contains(l, ": go") {
					return nil, bad(name, "holds build information for more than one binary (line %d). Pass one binary's output", i+1)
				}
				return nil, bad(name, "line %d isn't `go version -m` output: %q", i+1, clip(l))
			}
			l = strings.TrimPrefix(l, indent)
		}
		if strings.HasPrefix(l, "go\t") {
			return nil, bad(name, "holds build information for more than one binary (line %d). Pass one binary's output", i+1)
		}
		b.WriteString(l + "\n")
	}
	bi, err := debug.ParseBuildInfo(b.String())
	if err != nil {
		return nil, bad(name, "isn't valid build information: %v", err)
	}
	bi.GoVersion = goVersion // ParseBuildInfo skips the go line
	in := fromBuildInfo(bi)
	in.Name, in.Format = name, "go version -m"
	if err := validate(in); err != nil {
		return nil, err
	}
	return in, nil
}

func parseJSON(name string, data []byte) (*Info, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var bi debug.BuildInfo
	if err := dec.Decode(&bi); err != nil {
		return nil, bad(name, "isn't valid `go version -m -json` output, so it may be cut off: %v", err)
	}
	if dec.More() {
		return nil, bad(name, "holds build information for more than one binary. Pass one binary's output")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, bad(name, "has more data after the JSON object")
	}
	if bi.GoVersion == "" && bi.Path == "" {
		return nil, bad(name, "is JSON, but not `go version -m -json` output: it has no GoVersion or Path")
	}
	in := fromBuildInfo(&bi)
	in.Name, in.Format = name, "go version -m -json"
	if err := validate(in); err != nil {
		return nil, err
	}
	return in, nil
}

func clip(s string) string {
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

// archLevels is the setting Go writes after GOOS for each architecture, and
// the Go 1.x release that started writing it. It is the last setting before
// the VCS stamp, so its absence shows text cut off after GOOS.
var archLevels = map[string]struct {
	key   string
	since int
}{
	"amd64": {"GOAMD64", 18}, "386": {"GO386", 18}, "arm": {"GOARM", 18},
	"arm64": {"GOARM64", 23}, "riscv64": {"GORISCV64", 23},
	"mips": {"GOMIPS", 18}, "mipsle": {"GOMIPS", 18},
	"mips64": {"GOMIPS64", 18}, "mips64le": {"GOMIPS64", 18},
	"ppc64": {"GOPPC64", 18}, "ppc64le": {"GOPPC64", 18},
}

// validate refuses build information that is incomplete. Go writes the
// settings in a fixed order (flags, CGO, GOARCH, GOOS, CPU level, VCS), so
// text cut off at a line break loses the last of them.
func validate(in *Info) error {
	n := in.Name
	if !strings.HasPrefix(in.GoVersion, "go1") && !strings.HasPrefix(in.GoVersion, "devel") {
		return bad(n, "has an unexpected Go version %q", clip(in.GoVersion))
	}
	if in.Path == "" {
		return bad(n, "has no main package path")
	}
	for _, k := range []string{"-compiler", "GOARCH", "GOOS"} {
		if _, ok := in.Get(k); !ok {
			return bad(n, "has no %s build setting, so it is cut off or was built by Go older than 1.18, which this tool doesn't read", k)
		}
	}
	arch, _ := in.Get("GOARCH")
	if lvl, ok := archLevels[arch]; ok {
		if minor, ok := goMinor(in.GoVersion); ok && minor >= lvl.since {
			if _, ok := in.Get(lvl.key); !ok {
				return bad(n, "has no %s setting, which %s writes for %s, so it is cut off", lvl.key, in.GoVersion, arch)
			}
		}
	}
	if in.HasVCS() {
		if v, _ := in.Get("vcs.revision"); v == "" {
			return bad(n, "has a vcs setting but no vcs.revision, so it is cut off")
		}
		m, ok := in.Get("vcs.modified")
		if !ok {
			return bad(n, "has a vcs setting but no vcs.modified, so it is cut off")
		}
		if m != "true" && m != "false" {
			return bad(n, "has vcs.modified=%q, which should be true or false", m)
		}
		if t, ok := in.Get("vcs.time"); ok {
			if _, err := time.Parse(time.RFC3339Nano, t); err != nil {
				return bad(n, "has a vcs.time that isn't a time: %q", clip(t))
			}
		}
	}
	seen := map[string]bool{}
	for _, d := range in.Deps {
		if d.Path == "" || d.Version == "" {
			return bad(n, "has a dependency with no path or version")
		}
		if seen[d.Path] {
			return bad(n, "lists dependency %s twice", d.Path)
		}
		seen[d.Path] = true
		if d.Sum != "" && !strings.HasPrefix(d.Sum, "h1:") {
			return bad(n, "has a checksum for %s that isn't an h1: hash", d.Path)
		}
		if d.Replace != nil && d.Replace.Path == "" {
			return bad(n, "replaces %s with an empty path", d.Path)
		}
	}
	return nil
}

// goMinor returns 22 for go1.22.5, go1.22rc1 and go1.22.
func goMinor(v string) (int, bool) {
	major, minor, _, _, ok := parseGo(v)
	if !ok || major != 1 {
		return 0, false
	}
	return minor, true
}

// parseGo reads go1.22.5, go1.22rc1, go1.21 or "go1.22.5 X:boringcrypto".
// pre is 0 for a release, and negative for betas and release candidates.
func parseGo(v string) (major, minor, patch, pre int, ok bool) {
	v, _, _ = strings.Cut(v, " ")
	if !strings.HasPrefix(v, "go") {
		return
	}
	v = v[2:]
	for _, tag := range []string{"rc", "beta"} {
		if i := strings.Index(v, tag); i > 0 {
			n, err := strconv.Atoi(v[i+len(tag):])
			if err != nil {
				return
			}
			pre = -1000 + n
			if tag == "beta" {
				pre = -2000 + n
			}
			v = v[:i]
		}
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return
	}
	nums := []int{0, 0, 0}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], pre, true
}

// compareGo returns -1, 0 or 1, and false if either version can't be read.
func compareGo(a, b string) (int, bool) {
	a1, a2, a3, a4, ok1 := parseGo(a)
	b1, b2, b3, b4, ok2 := parseGo(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	for _, p := range [][2]int{{a1, b1}, {a2, b2}, {a3, b3}, {a4, b4}} {
		if p[0] != p[1] {
			if p[0] < p[1] {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

// compareSemver compares module versions such as v1.2.3, v0.0.0-2024...-abc
// (pseudo-versions sort by time by design) and v2.0.0+incompatible.
func compareSemver(a, b string) (int, bool) {
	// Pseudo-versions from commits made in the same second differ only in
	// the commit hash, which says nothing about order.
	if a != b && pseudoTime.MatchString(a) && pseudoTime.ReplaceAllString(a, "${1}${2}") == pseudoTime.ReplaceAllString(b, "${1}${2}") {
		return 0, false
	}
	pa, ok1 := parseSemver(a)
	pb, ok2 := parseSemver(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	for i := 0; i < 3; i++ {
		if pa.nums[i] != pb.nums[i] {
			if pa.nums[i] < pb.nums[i] {
				return -1, true
			}
			return 1, true
		}
	}
	switch {
	case pa.pre == pb.pre:
		return 0, true
	case pa.pre == "":
		return 1, true
	case pb.pre == "":
		return -1, true
	}
	x, y := strings.Split(pa.pre, "."), strings.Split(pb.pre, ".")
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] == y[i] {
			continue
		}
		nx, ex := strconv.ParseUint(x[i], 10, 64)
		ny, ey := strconv.ParseUint(y[i], 10, 64)
		switch {
		case ex == nil && ey == nil:
			if nx < ny {
				return -1, true
			}
			return 1, true
		case ex == nil:
			return -1, true
		case ey == nil:
			return 1, true
		case x[i] < y[i]:
			return -1, true
		default:
			return 1, true
		}
	}
	switch {
	case len(x) < len(y):
		return -1, true
	case len(x) > len(y):
		return 1, true
	}
	return 0, true
}

// pseudoTime matches the commit hash at the end of a pseudo-version, after
// its 14-digit timestamp.
var pseudoTime = regexp.MustCompile(`([-.]\d{14})-[0-9a-f]{12}((?:\+incompatible)?)$`)

type semver struct {
	nums [3]uint64
	pre  string
}

func parseSemver(v string) (semver, bool) {
	var s semver
	if !strings.HasPrefix(v, "v") {
		return s, false
	}
	v = v[1:]
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		s.pre = v[i+1:]
		v = v[:i]
		if s.pre == "" {
			return s, false
		}
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return s, false
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil || p == "" {
			return s, false
		}
		s.nums[i] = n
	}
	return s, true
}

// Finding is a change that needs a person to look at it. These make --fail
// exit 1.
type Finding struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// Change is an informational change between base and head.
type Change struct {
	What string `json:"what"`
	Old  string `json:"old,omitempty"`
	New  string `json:"new,omitempty"`
}

// DepChange is one dependency that differs.
type DepChange struct {
	Path   string `json:"path"`
	Change string `json:"change"`
	Old    string `json:"old,omitempty"`
	New    string `json:"new,omitempty"`
}

// Report is the result of comparing two binaries.
type Report struct {
	Base      *Info       `json:"base"`
	Head      *Info       `json:"head"`
	Same      bool        `json:"same"`
	Findings  []Finding   `json:"findings"`
	Toolchain *Change     `json:"toolchain,omitempty"`
	Deps      []DepChange `json:"deps"`
	Settings  []Change    `json:"settings"`
}

func isLocalPath(p string) bool {
	return strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") ||
		strings.HasPrefix(p, `.\`) || strings.HasPrefix(p, `..\`) || (len(p) > 2 && p[1] == ':' && (p[2] == '\\' || p[2] == '/'))
}

func modString(m *Module) string {
	if m == nil {
		return ""
	}
	if m.Version == "" || m.Version == "(devel)" {
		return m.Path
	}
	return m.Path + " " + m.Version
}

// Compare builds the report. Base is the binary you trust, usually the one
// running now; head is the one about to replace it.
func Compare(base, head *Info) *Report {
	r := &Report{Base: base, Head: head, Findings: []Finding{}, Deps: []DepChange{}, Settings: []Change{}}
	add := func(kind, format string, args ...any) {
		r.Findings = append(r.Findings, Finding{Kind: kind, Text: fmt.Sprintf(format, args...)})
	}

	// Identity: is this the same program on the same platform?
	if base.Path != head.Path {
		add("different-program", "The main package differs: %s in base, %s in head. Check that you compared two builds of the same program", base.Path, head.Path)
	}
	bOS, _ := base.Get("GOOS")
	bArch, _ := base.Get("GOARCH")
	hOS, _ := head.Get("GOOS")
	hArch, _ := head.Get("GOARCH")
	if bOS != hOS || bArch != hArch {
		add("platform", "The platform differs: %s/%s in base, %s/%s in head, so build settings below may differ only for that reason", bOS, bArch, hOS, hArch)
	}

	// Toolchain.
	if base.GoVersion != head.GoVersion {
		r.Toolchain = &Change{What: "Go", Old: base.GoVersion, New: head.GoVersion}
		if c, ok := compareGo(base.GoVersion, head.GoVersion); ok && c > 0 {
			add("go-downgrade", "Go toolchain went down from %s to %s", base.GoVersion, head.GoVersion)
		}
	}

	// VCS stamp.
	if m, _ := head.Get("vcs.modified"); m == "true" {
		rev, _ := head.Get("vcs.revision")
		add("dirty", "Head was built from a working tree with uncommitted changes (vcs.modified=true), so revision %s doesn't identify its code", short(rev))
	}
	if base.HasVCS() && !head.HasVCS() {
		add("no-vcs", "Head has no VCS stamp, but base has one. It was built with -buildvcs=false or outside the repository, for example in a Docker build without .git, so nothing ties it to a commit")
	}
	bt, bok := settingTime(base)
	ht, hok := settingTime(head)
	if bok && hok && ht.Before(bt) {
		add("older-commit", "Head's commit (%s) is older than base's (%s). Check the argument order, or whether this is a rollback", ht.Format("2006-01-02"), bt.Format("2006-01-02"))
	}
	if c, ok := compareSemver(trimDirty(base.Main.Version), trimDirty(head.Main.Version)); ok && c > 0 {
		add("older-version", "Head's module version %s is lower than base's %s", head.Main.Version, base.Main.Version)
	}

	compareDeps(r, base, head, add)
	compareSettings(r, base, head, add)

	r.Same = len(r.Findings) == 0 && r.Toolchain == nil && len(r.Deps) == 0 && len(r.Settings) == 0 &&
		base.Main.Path == head.Main.Path && base.Main.Version == head.Main.Version && base.Main.Sum == head.Main.Sum && sameSettings(base, head)
	return r
}

// sameSettings compares every setting, including the VCS stamp, which the
// report shows in its header instead of listing.
func sameSettings(a, b *Info) bool {
	if len(a.Settings) != len(b.Settings) {
		return false
	}
	for _, s := range a.Settings {
		if v, ok := b.Get(s.Key); !ok || v != s.Value {
			return false
		}
	}
	return true
}

func trimDirty(v string) string { return strings.TrimSuffix(v, "+dirty") }

func short(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
}

func settingTime(in *Info) (time.Time, bool) {
	v, ok := in.Get("vcs.time")
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	return t, err == nil
}

func compareDeps(r *Report, base, head *Info, add func(kind, format string, args ...any)) {
	bm := map[string]Module{}
	for _, d := range base.Deps {
		bm[d.Path] = d
	}
	hm := map[string]Module{}
	for _, d := range head.Deps {
		hm[d.Path] = d
	}
	var paths []string
	for p := range bm {
		paths = append(paths, p)
	}
	for p := range hm {
		if _, ok := bm[p]; !ok {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		b, inB := bm[p]
		h, inH := hm[p]
		switch {
		case !inH:
			r.Deps = append(r.Deps, DepChange{Path: p, Change: "removed", Old: b.Version})
			continue
		case !inB:
			r.Deps = append(r.Deps, DepChange{Path: p, Change: "added", New: h.Version})
			if h.Replace != nil {
				newReplace(add, h)
			}
			continue
		}
		if b.Version != h.Version {
			c, ok := compareSemver(b.Version, h.Version)
			switch {
			case !ok:
				r.Deps = append(r.Deps, DepChange{Path: p, Change: "changed", Old: b.Version, New: h.Version})
			case c < 0:
				r.Deps = append(r.Deps, DepChange{Path: p, Change: "upgraded", Old: b.Version, New: h.Version})
			case c > 0:
				r.Deps = append(r.Deps, DepChange{Path: p, Change: "downgraded", Old: b.Version, New: h.Version})
				add("downgrade", "%s went down from %s to %s", p, b.Version, h.Version)
			default:
				r.Deps = append(r.Deps, DepChange{Path: p, Change: "changed", Old: b.Version, New: h.Version})
			}
		} else if b.Sum != "" && h.Sum != "" && b.Sum != h.Sum && b.Replace == nil && h.Replace == nil {
			r.Deps = append(r.Deps, DepChange{Path: p, Change: "checksum changed", Old: b.Sum, New: h.Sum})
			add("checksum", "%s %s has a different checksum in head (%s, was %s). The same version should always have the same content", p, h.Version, h.Sum, b.Sum)
		}
		switch {
		case b.Replace == nil && h.Replace != nil:
			r.Deps = append(r.Deps, DepChange{Path: p, Change: "now replaced", New: modString(h.Replace)})
			newReplace(add, h)
		case b.Replace != nil && h.Replace == nil:
			r.Deps = append(r.Deps, DepChange{Path: p, Change: "no longer replaced", Old: modString(b.Replace)})
		case b.Replace != nil && h.Replace != nil && *b.Replace != *h.Replace:
			r.Deps = append(r.Deps, DepChange{Path: p, Change: "replacement changed", Old: modString(b.Replace), New: modString(h.Replace)})
			if b.Replace.Path != h.Replace.Path || isLocalPath(h.Replace.Path) {
				newReplace(add, h)
			} else if c, ok := compareSemver(b.Replace.Version, h.Replace.Version); ok && c > 0 {
				add("downgrade", "%s's replacement %s went down from %s to %s", p, h.Replace.Path, b.Replace.Version, h.Replace.Version)
			} else if b.Replace.Version == h.Replace.Version && b.Replace.Sum != "" && h.Replace.Sum != "" {
				add("checksum", "%s's replacement %s %s has a different checksum in head (%s, was %s)", p, h.Replace.Path, h.Replace.Version, h.Replace.Sum, b.Replace.Sum)
			}
		}
	}
}

func newReplace(add func(kind, format string, args ...any), h Module) {
	if isLocalPath(h.Replace.Path) {
		add("replace", "%s is replaced by the local directory %s in head, so its code isn't pinned by a version or checksum", h.Path, h.Replace.Path)
		return
	}
	add("replace", "%s is replaced by %s in head", h.Path, modString(h.Replace))
}

// infoSettings change with every build or don't change behavior, so a change
// is listed without needing a look.
var infoSettings = map[string]bool{
	"CGO_CFLAGS": true, "CGO_CPPFLAGS": true, "CGO_CXXFLAGS": true, "CGO_LDFLAGS": true,
	"-pgo": true,
}

func compareSettings(r *Report, base, head *Info, add func(kind, format string, args ...any)) {
	bs := map[string]string{}
	var keys []string
	for _, s := range base.Settings {
		bs[s.Key] = s.Value
		keys = append(keys, s.Key)
	}
	hs := map[string]string{}
	for _, s := range head.Settings {
		hs[s.Key] = s.Value
		if _, ok := bs[s.Key]; !ok {
			keys = append(keys, s.Key)
		}
	}
	bOS, hOS := bs["GOOS"], hs["GOOS"]
	for _, k := range keys {
		bv, inB := bs[k]
		hv, inH := hs[k]
		if inB == inH && bv == hv {
			continue
		}
		show := func(v string, ok bool) string {
			if !ok {
				return "unset"
			}
			if v == "" {
				return `""`
			}
			return v
		}
		switch {
		case strings.HasPrefix(k, "vcs"):
			// Shown in the header for each side.
		case infoSettings[k] && bv == "" && hv == "":
			// CGO_CFLAGS= and friends appear empty whenever cgo is on.
		case infoSettings[k]:
			r.Settings = append(r.Settings, Change{What: k, Old: show(bv, inB), New: show(hv, inH)})
		case k == "GOOS" || k == "GOARCH":
			// Reported once as a platform finding.
		case k == "-ldflags" && (!inB && bs["-trimpath"] == "true" || !inH && hs["-trimpath"] == "true"):
			// Go leaves -ldflags out of the build information when -trimpath
			// is set, so a missing value says nothing about the flags used.
			r.Settings = append(r.Settings, Change{What: k, Old: ldflagsOrUnrecorded(bv, inB), New: ldflagsOrUnrecorded(hv, inH)})
		case k == "-ldflags":
			compareLdflags(r, bv, hv, add)
		case k == "-trimpath":
			if bv == "true" && hv != "true" {
				add("trimpath", "-trimpath was removed, so head embeds the build machine's file paths")
			} else {
				r.Settings = append(r.Settings, Change{What: k, Old: show(bv, inB), New: show(hv, inH)})
			}
		case k == "CGO_ENABLED":
			if hv == "1" && hOS == "linux" {
				add("cgo", "CGO_ENABLED changed from %s to %s. A Linux binary built with cgo usually needs the C library at run time, so it may fail in scratch or distroless images", show(bv, inB), show(hv, inH))
			} else {
				add("cgo", "CGO_ENABLED changed from %s to %s", show(bv, inB), show(hv, inH))
			}
		case k == "-tags":
			gone, added := diffList(strings.Split(bv, ","), strings.Split(hv, ","))
			add("tags", "Build tags changed: %s", describeListChange(gone, added))
		case k == "DefaultGODEBUG":
			add("godebug", "DefaultGODEBUG changed, which changes runtime behavior without a code change: %s", describeGodebug(bv, hv))
		case k == "-race" || k == "-msan" || k == "-asan":
			if hv == "true" {
				add("instrumented", "Head was built with %s, which is meant for testing: it is slower and uses more memory", k)
			} else {
				r.Settings = append(r.Settings, Change{What: k, Old: show(bv, inB), New: show(hv, inH)})
			}
		case k == "-gcflags" && (hasFlag(hv, "-N") || hasFlag(hv, "-l")) && !(hasFlag(bv, "-N") || hasFlag(bv, "-l")):
			add("debug-build", "-gcflags changed from %s to %s, which turns off optimizations or inlining", show(bv, inB), show(hv, inH))
		case archLevelKey(k):
			if bOS != hOS || bs["GOARCH"] != hs["GOARCH"] {
				break // reported as a platform change
			}
			if (!inB && predatesLevel(base)) || (!inH && predatesLevel(head)) {
				break // an older Go didn't write the level, so unset means the default
			}
			add("cpu-level", "%s changed from %s to %s. Check that every machine that runs head supports the new level", k, show(bv, inB), show(hv, inH))
		default:
			add("build-setting", "Build setting %s changed from %s to %s", k, show(bv, inB), show(hv, inH))
		}
	}
}

// predatesLevel reports whether the Go that built in was older than the
// release that started writing its architecture's level setting.
func predatesLevel(in *Info) bool {
	arch, _ := in.Get("GOARCH")
	lvl, ok := archLevels[arch]
	minor, ok2 := goMinor(in.GoVersion)
	return ok && ok2 && minor < lvl.since
}

func ldflagsOrUnrecorded(v string, ok bool) string {
	if !ok {
		return "not recorded (Go leaves it out with -trimpath)"
	}
	return v
}

func archLevelKey(k string) bool {
	for _, l := range archLevels {
		if l.key == k {
			return true
		}
	}
	return false
}

func hasFlag(v, f string) bool {
	for _, t := range strings.Fields(v) {
		if t == f || strings.HasSuffix(t, "="+f) {
			return true
		}
	}
	return false
}

func diffList(old, new []string) (gone, added []string) {
	in := func(list []string, s string) bool {
		for _, x := range list {
			if x == s {
				return true
			}
		}
		return false
	}
	for _, o := range old {
		if o != "" && !in(new, o) {
			gone = append(gone, o)
		}
	}
	for _, n := range new {
		if n != "" && !in(old, n) {
			added = append(added, n)
		}
	}
	return gone, added
}

func describeListChange(gone, added []string) string {
	var parts []string
	if len(gone) > 0 {
		parts = append(parts, "removed "+strings.Join(gone, ", "))
	}
	if len(added) > 0 {
		parts = append(parts, "added "+strings.Join(added, ", "))
	}
	if len(parts) == 0 {
		return "same tags in a different order"
	}
	return strings.Join(parts, "; ")
}

func describeGodebug(old, new string) string {
	parse := func(s string) (map[string]string, []string) {
		m := map[string]string{}
		var keys []string
		for _, kv := range strings.Split(s, ",") {
			if kv == "" {
				continue
			}
			k, v, _ := strings.Cut(kv, "=")
			m[k] = v
			keys = append(keys, k)
		}
		return m, keys
	}
	om, okeys := parse(old)
	nm, nkeys := parse(new)
	var gone, added, changed []string
	for _, k := range okeys {
		if v, ok := nm[k]; !ok {
			gone = append(gone, k+"="+om[k])
		} else if v != om[k] {
			changed = append(changed, k+" "+om[k]+" -> "+v)
		}
	}
	for _, k := range nkeys {
		if _, ok := om[k]; !ok {
			added = append(added, k+"="+nm[k])
		}
	}
	var parts []string
	if len(gone) > 0 {
		parts = append(parts, "no longer set "+strings.Join(gone, ", "))
	}
	if len(added) > 0 {
		parts = append(parts, "newly set "+strings.Join(added, ", "))
	}
	if len(changed) > 0 {
		parts = append(parts, "changed "+strings.Join(changed, ", "))
	}
	if len(parts) == 0 {
		return "same values in a different order"
	}
	return strings.Join(parts, "; ")
}

// splitLdflags separates -X name=value pairs, which usually stamp a version
// and change with every release, from the other linker flags.
func splitLdflags(s string) (xs map[string]string, xkeys []string, other []string) {
	xs = map[string]string{}
	toks := shellFields(s)
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		var kv string
		switch {
		case (t == "-X" || t == "--X") && i+1 < len(toks):
			kv = toks[i+1]
			i++
		case strings.HasPrefix(t, "-X="):
			kv = t[3:]
		default:
			other = append(other, t)
			continue
		}
		k, v, _ := strings.Cut(kv, "=")
		if _, dup := xs[k]; !dup {
			xkeys = append(xkeys, k)
		}
		xs[k] = v
	}
	return xs, xkeys, other
}

// shellFields splits on spaces, keeping quoted parts together the way the go
// command reads -ldflags.
func shellFields(s string) []string {
	var out []string
	var cur strings.Builder
	in := false
	var quote rune
	quoted := false
	for _, c := range s {
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteRune(c)
			}
		case c == '\'' || c == '"':
			quote, in, quoted = c, true, true
		case c == ' ' || c == '\t' || c == '\n':
			if in {
				out = append(out, cur.String())
				cur.Reset()
				in, quoted = false, false
			}
		default:
			cur.WriteRune(c)
			in = true
		}
	}
	if in || quoted {
		out = append(out, cur.String())
	}
	return out
}

func compareLdflags(r *Report, old, new string, add func(kind, format string, args ...any)) {
	ox, okeys, oo := splitLdflags(old)
	nx, nkeys, no := splitLdflags(new)
	if strings.Join(oo, "\x00") != strings.Join(no, "\x00") {
		gone, added := diffList(oo, no)
		desc := describeListChange(gone, added)
		if len(gone) == 0 && len(added) == 0 {
			desc = fmt.Sprintf("%q -> %q", strings.Join(oo, " "), strings.Join(no, " "))
		}
		add("ldflags", "Linker flags other than -X changed: %s", desc)
	}
	for _, k := range okeys {
		v, ok := nx[k]
		switch {
		case !ok:
			r.Settings = append(r.Settings, Change{What: "-ldflags -X " + k, Old: ox[k], New: "unset"})
		case v != ox[k]:
			r.Settings = append(r.Settings, Change{What: "-ldflags -X " + k, Old: ox[k], New: v})
		}
	}
	for _, k := range nkeys {
		if _, ok := ox[k]; !ok {
			r.Settings = append(r.Settings, Change{What: "-ldflags -X " + k, Old: "unset", New: nx[k]})
		}
	}
}

func describeSide(in *Info) string {
	parts := []string{modString(&in.Main)}
	if in.Main.Path == "" {
		parts[0] = in.Path
	} else if in.Main.Version == "(devel)" {
		parts[0] += " (devel)"
	}
	os, _ := in.Get("GOOS")
	arch, _ := in.Get("GOARCH")
	parts = append(parts, in.GoVersion, os+"/"+arch)
	if in.HasVCS() {
		rev, _ := in.Get("vcs.revision")
		s := short(rev)
		if t, ok := settingTime(in); ok {
			s += " " + t.UTC().Format("2006-01-02")
		}
		if m, _ := in.Get("vcs.modified"); m == "true" {
			s += " modified"
		}
		parts = append(parts, s)
	} else {
		parts = append(parts, "no VCS stamp")
	}
	return strings.Join(parts, ", ")
}

// WriteText prints the report for people.
func (r *Report) WriteText(w io.Writer) {
	if r.Same {
		fmt.Fprintf(w, "No differences: same Go version, module version, %d dependencies and build settings.\n", len(r.Head.Deps))
		return
	}
	fmt.Fprintf(w, "base  %s\n      %s\n", r.Base.Name, describeSide(r.Base))
	fmt.Fprintf(w, "head  %s\n      %s\n\n", r.Head.Name, describeSide(r.Head))
	switch len(r.Findings) {
	case 0:
		fmt.Fprintln(w, "Nothing needs a look.")
	case 1:
		fmt.Fprintln(w, "1 change needs a look:")
	default:
		fmt.Fprintf(w, "%d changes need a look:\n", len(r.Findings))
	}
	for _, f := range r.Findings {
		fmt.Fprintf(w, "  - %s.\n", f.Text)
	}
	if r.Toolchain != nil {
		fmt.Fprintf(w, "\nGo: %s -> %s\n", r.Toolchain.Old, r.Toolchain.New)
	}
	if r.Base.Main.Version != r.Head.Main.Version {
		fmt.Fprintf(w, "\nModule version: %s -> %s\n", r.Base.Main.Version, r.Head.Main.Version)
	}
	if len(r.Deps) > 0 {
		counts := map[string]int{}
		var order []string
		for _, d := range r.Deps {
			if counts[d.Change] == 0 {
				order = append(order, d.Change)
			}
			counts[d.Change]++
		}
		sort.SliceStable(order, func(i, j int) bool { return depOrder(order[i]) < depOrder(order[j]) })
		var sum []string
		for _, c := range order {
			sum = append(sum, fmt.Sprintf("%d %s", counts[c], c))
		}
		fmt.Fprintf(w, "\nDependencies: %s\n", strings.Join(sum, ", "))
		deps := append([]DepChange(nil), r.Deps...)
		sort.SliceStable(deps, func(i, j int) bool { return depOrder(deps[i].Change) < depOrder(deps[j].Change) })
		width := 0
		for _, d := range deps {
			if len(d.Change) > width {
				width = len(d.Change)
			}
		}
		for _, d := range deps {
			switch {
			case d.Old != "" && d.New != "":
				fmt.Fprintf(w, "  %-*s  %s  %s -> %s\n", width, d.Change, d.Path, d.Old, d.New)
			case d.New != "":
				fmt.Fprintf(w, "  %-*s  %s  %s\n", width, d.Change, d.Path, d.New)
			default:
				fmt.Fprintf(w, "  %-*s  %s  %s\n", width, d.Change, d.Path, d.Old)
			}
		}
	}
	if len(r.Settings) > 0 {
		fmt.Fprintln(w, "\nOther build settings:")
		for _, s := range r.Settings {
			fmt.Fprintf(w, "  %s: %s -> %s\n", s.What, s.Old, s.New)
		}
	}
}

func depOrder(c string) int {
	switch c {
	case "downgraded":
		return 0
	case "checksum changed":
		return 1
	case "now replaced", "replacement changed":
		return 2
	case "upgraded":
		return 3
	case "changed":
		return 4
	case "added":
		return 5
	case "removed":
		return 6
	}
	return 7
}

// checkTextEnds catches `go version -m` text cut off just before its VCS
// lines, which looks exactly like a build without a VCS stamp. A build from
// the module proxy (go install pkg@version) records a checksum for the main
// module and never has VCS lines, so only text from a local build without
// them is in doubt. Binaries and JSON can't be cut off that way unnoticed.
func checkTextEnds(base, head *Info) error {
	for _, p := range [][2]*Info{{base, head}, {head, base}} {
		in, other := p[0], p[1]
		if in.Format != "go version -m" || in.HasVCS() || in.Main.Path == "" {
			continue
		}
		if in.Main.Sum == "" || other.HasVCS() {
			return bad(in.Name, "has no VCS lines, though it was built from a local directory. Either it was built without a VCS stamp (-buildvcs=false, or outside the repository), or the text was cut off just before its vcs lines, and the two look the same. Check the end of the file, or pass the binary itself. If the build really has no stamp, run again with --trust-no-vcs")
		}
	}
	return nil
}

const usage = `buildinfodiff compares the build information Go embeds in two binaries.

Usage:
  buildinfodiff [--fail] [--json] [--trust-no-vcs] BASE HEAD

BASE and HEAD are Go executables (ELF, Mach-O or PE), or saved output of
"go version -m FILE" or "go version -m -json FILE". Use - for standard input.
BASE is the build you trust, usually the one running now. HEAD replaces it.

  --fail          exit 1 if any change needs a look
  --json          print the full result as JSON
  --trust-no-vcs  accept go version -m text from a local build that has no
                  VCS lines, after checking it isn't cut off
  --version       print the version

Exit codes: 0 compared, 1 a change needs a look (with --fail),
2 bad usage or an input that can't be trusted.
`

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("buildinfodiff", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fail := fs.Bool("fail", false, "")
	asJSON := fs.Bool("json", false, "")
	trustNoVCS := fs.Bool("trust-no-vcs", false, "")
	showVersion := fs.Bool("version", false, "")
	help := fs.Bool("help", false, "")
	if err := fs.Parse(reorder(args)); err != nil {
		fmt.Fprintf(stderr, "buildinfodiff: %v\n\n%s", err, usage)
		return 2
	}
	if *help {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if *showVersion {
		fmt.Fprintln(stdout, "buildinfodiff", version)
		return 0
	}
	if fs.NArg() != 2 {
		fmt.Fprintf(stderr, "buildinfodiff: need two inputs, BASE and HEAD, got %d\n\n%s", fs.NArg(), usage)
		return 2
	}
	if fs.Arg(0) == "-" && fs.Arg(1) == "-" {
		fmt.Fprintln(stderr, "buildinfodiff: only one input can be standard input")
		return 2
	}
	var infos [2]*Info
	for i, side := range []string{"base", "head"} {
		in, err := Load(fs.Arg(i))
		if err != nil {
			fmt.Fprintf(stderr, "buildinfodiff: %s can't be trusted, so nothing was compared.\n  %v\n", side, err)
			return 2
		}
		infos[i] = in
	}
	if !*trustNoVCS {
		if err := checkTextEnds(infos[0], infos[1]); err != nil {
			fmt.Fprintf(stderr, "buildinfodiff: an input may be cut off, so nothing was compared.\n  %v\n", err)
			return 2
		}
	}
	r := Compare(infos[0], infos[1])
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(r); err != nil {
			fmt.Fprintf(stderr, "buildinfodiff: %v\n", err)
			return 2
		}
	} else {
		r.WriteText(stdout)
	}
	if *fail && len(r.Findings) > 0 {
		return 1
	}
	return 0
}

// reorder moves flags before positional arguments, so flags can come last.
func reorder(args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			continue
		}
		pos = append(pos, a)
	}
	return append(append(flags, "--"), pos...)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
