package main

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	realDir = "../../examples/buildinfo/real"
	exDir   = "../../examples/buildinfo"
	badDir  = "../../tests/testdata/bad"
)

func runCLI(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func load(t *testing.T, path string) *Info {
	t.Helper()
	in, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func kinds(r *Report) []string {
	var k []string
	for _, f := range r.Findings {
		k = append(k, f.Kind)
	}
	sort.Strings(k)
	return k
}

func wantKinds(t *testing.T, r *Report, want ...string) {
	t.Helper()
	sort.Strings(want)
	if got := kinds(r); strings.Join(got, ",") != strings.Join(want, ",") {
		var b strings.Builder
		r.WriteText(&b)
		t.Fatalf("finding kinds %v, want %v\n%s", got, want, b.String())
	}
}

func TestRealReleases(t *testing.T) {
	cases := []struct {
		base, head string
		kinds      []string
		contains   []string
	}{
		{"node_exporter-1.8.2-linux-arm64.txt", "node_exporter-1.9.1-linux-arm64.txt", []string{"godebug"},
			[]string{"no longer set httplaxcontentlength=1", "upgraded  golang.org/x/net  v0.23.0 -> v0.37.0", "removed   github.com/go-kit/log  v0.2.1",
				"-ldflags -X github.com/prometheus/common/version.Version: 1.8.2 -> 1.9.1"}},
		{"node_exporter-1.9.1-linux-arm64.txt", "node_exporter-1.10.2-linux-arm64.txt", []string{"godebug"},
			[]string{"Go: go1.23.7 -> go1.25.3", "Module version: (devel) -> v1.10.2", "added     go.yaml.in/yaml/v2  v2.4.3"}},
		{"kubectl-1.31.4-linux-arm64.txt", "kubectl-1.32.0-linux-arm64.txt", []string{"godebug"},
			[]string{"newly set winsymlink=0", "Dependencies: 22 upgraded, 2 added, 4 removed"}},
		{"node_exporter-1.9.1-linux-arm64.txt", "node_exporter-1.9.1-linux-amd64.txt", []string{"platform"}, nil},
		{"stringer-v0.36.0-go-install.txt", "stringer-v0.38.0-go-install.txt", []string{"godebug"},
			[]string{"no VCS stamp", "Module version: v0.36.0 -> v0.38.0"}},
		// Reversed: the older release as head.
		{"node_exporter-1.9.1-linux-arm64.txt", "node_exporter-1.8.2-linux-arm64.txt", []string{"godebug", "go-downgrade", "older-commit", "downgrade", "downgrade", "downgrade", "downgrade", "downgrade", "downgrade", "downgrade", "downgrade", "downgrade", "downgrade", "downgrade", "downgrade", "downgrade", "downgrade", "downgrade", "downgrade", "downgrade", "downgrade"}, nil},
		{"stringer-v0.38.0-go-install.txt", "stringer-v0.36.0-go-install.txt", []string{"godebug", "older-version", "downgrade", "downgrade"}, nil},
	}
	for _, c := range cases {
		t.Run(c.base+"_"+c.head, func(t *testing.T) {
			r := Compare(load(t, filepath.Join(realDir, c.base)), load(t, filepath.Join(realDir, c.head)))
			wantKinds(t, r, c.kinds...)
			var b strings.Builder
			r.WriteText(&b)
			for _, s := range c.contains {
				if !strings.Contains(b.String(), s) {
					t.Errorf("missing %q in\n%s", s, b.String())
				}
			}
		})
	}
}

func TestOrdersExamples(t *testing.T) {
	code, out, _ := runCLI("--fail", exDir+"/orders-v1.4.0.txt", exDir+"/orders-v1.4.1.txt")
	if code != 0 || !strings.Contains(out, "Nothing needs a look.") {
		t.Fatalf("comment-only release: code %d\n%s", code, out)
	}
	r := Compare(load(t, exDir+"/orders-v1.4.1.txt"), load(t, exDir+"/orders-hotfix.txt"))
	wantKinds(t, r, "dirty", "replace", "downgrade", "tags", "trimpath", "godebug", "cgo")
	code, _, _ = runCLI("--fail", exDir+"/orders-v1.4.1.txt", exDir+"/orders-hotfix.txt")
	if code != 1 {
		t.Fatalf("hotfix with --fail: code %d", code)
	}
	code, _, _ = runCLI(exDir+"/orders-v1.4.1.txt", exDir+"/orders-hotfix.txt")
	if code != 0 {
		t.Fatalf("hotfix without --fail: code %d", code)
	}
}

func TestSameInputIsNoDifferences(t *testing.T) {
	p := filepath.Join(realDir, "node_exporter-1.9.1-linux-arm64.txt")
	code, out, _ := runCLI("--fail", p, p)
	if code != 0 || !strings.HasPrefix(out, "No differences: same Go version, module version, 45 dependencies") {
		t.Fatalf("code %d\n%s", code, out)
	}
}

func TestJSONAndTextAgree(t *testing.T) {
	for _, v := range []string{"1.31.4", "1.32.0"} {
		j := load(t, filepath.Join(realDir, "kubectl-"+v+"-linux-arm64.json"))
		x := load(t, filepath.Join(realDir, "kubectl-"+v+"-linux-arm64.txt"))
		if j.Format != "go version -m -json" || x.Format != "go version -m" {
			t.Fatalf("formats %q %q", j.Format, x.Format)
		}
		if r := Compare(j, x); !r.Same {
			t.Fatalf("kubectl %s: JSON and text differ: %+v", v, r)
		}
	}
}

func TestStdin(t *testing.T) {
	old := os.Stdin
	defer func() { os.Stdin = old }()
	f, err := os.Open(exDir + "/orders-v1.4.1.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	os.Stdin = f
	code, out, _ := runCLI(exDir+"/orders-v1.4.0.txt", "-")
	if code != 0 || !strings.Contains(out, "head  standard input") {
		t.Fatalf("code %d\n%s", code, out)
	}
}

// checkRefused runs base/head with and without --fail and requires exit 2,
// no report on stdout, and a message saying the input can't be trusted.
func checkRefused(t *testing.T, label, base, head string) {
	t.Helper()
	for _, fail := range []bool{false, true} {
		args := []string{base, head}
		if fail {
			args = append([]string{"--fail"}, args...)
		}
		code, out, errOut := runCLI(args...)
		if code != 2 || out != "" || !(strings.Contains(errOut, "can't be trusted") || strings.Contains(errOut, "may be cut off")) {
			t.Fatalf("%s (fail=%v): code %d\nstdout:\n%s\nstderr:\n%s", label, fail, code, out, errOut)
		}
	}
}

func TestBadInputsOnEachSide(t *testing.T) {
	entries, err := os.ReadDir(badDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 12 {
		t.Fatalf("only %d bad inputs", len(entries))
	}
	good := exDir + "/orders-v1.4.1.txt"
	for _, e := range entries {
		p := filepath.Join(badDir, e.Name())
		checkRefused(t, e.Name()+" as base", p, good)
		checkRefused(t, e.Name()+" as head", good, p)
	}
	checkRefused(t, "directory", good, badDir)
	checkRefused(t, "missing file", good, badDir+"/no-such-file")
}

func writeTemp(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestCutTextIsNeverTrusted cuts real go version -m output at every line
// break and at points inside lines, on each side, with the other side whole.
func TestCutTextIsNeverTrusted(t *testing.T) {
	pairs := [][2]string{
		{realDir + "/node_exporter-1.8.2-linux-arm64.txt", realDir + "/node_exporter-1.9.1-linux-arm64.txt"},
		{realDir + "/kubectl-1.31.4-linux-arm64.txt", realDir + "/kubectl-1.32.0-linux-arm64.txt"},
		{realDir + "/stringer-v0.36.0-go-install.txt", realDir + "/stringer-v0.38.0-go-install.txt"},
		{exDir + "/orders-v1.4.0.txt", exDir + "/orders-v1.4.1.txt"},
		{exDir + "/orders-v1.4.1.txt", exDir + "/orders-hotfix.txt"},
	}
	dir := t.TempDir()
	cuts := 0
	for _, p := range pairs {
		for side := 0; side < 2; side++ {
			data := read(t, p[side])
			var points []int
			for i, c := range data {
				if c == '\n' && i+1 < len(data) {
					points = append(points, i+1)
				}
			}
			step := len(data) / 40
			if testing.Short() {
				step = len(data) / 8
			}
			for i := 1; i < len(data); i += step {
				points = append(points, i)
			}
			for _, n := range points {
				cut := writeTemp(t, dir, "cut.txt", data[:n])
				args := [2]string{p[0], p[1]}
				args[side] = cut
				checkRefused(t, p[side]+" cut at "+strconv.Itoa(n), args[0], args[1])
				cuts++
			}
		}
	}
	t.Logf("%d cut inputs refused", cuts)
}

func TestCutJSONIsNeverTrusted(t *testing.T) {
	dir := t.TempDir()
	good := realDir + "/kubectl-1.31.4-linux-arm64.json"
	data := read(t, realDir+"/kubectl-1.32.0-linux-arm64.json")
	trimmed := bytes.TrimRight(data, "\n")
	for n := 1; n < len(trimmed); n += 37 {
		cut := writeTemp(t, dir, "cut.json", data[:n])
		checkRefused(t, "json cut at "+strconv.Itoa(n)+" as head", good, cut)
		checkRefused(t, "json cut at "+strconv.Itoa(n)+" as base", cut, good)
	}
}

// Text cut just before its vcs lines looks like a build without a VCS stamp.
// It is refused unless --trust-no-vcs says the build really has no stamp.
func TestTextWithoutVCSNeedsTrust(t *testing.T) {
	dir := t.TempDir()
	data := string(read(t, exDir+"/orders-v1.4.1.txt"))
	i := strings.Index(data, "\tbuild\tvcs=git\n")
	if i < 0 {
		t.Fatal("no vcs line")
	}
	noVCS := writeTemp(t, dir, "novcs.txt", []byte(data[:i]))
	base := exDir + "/orders-v1.4.0.txt"
	checkRefused(t, "no vcs as head", base, noVCS)
	checkRefused(t, "no vcs as base", noVCS, base)
	// The other side has no VCS stamp either, because it came from the
	// module proxy. The cut text still can't be trusted.
	proxyBuild := realDir + "/stringer-v0.38.0-go-install.txt"
	checkRefused(t, "no vcs vs go install as head", proxyBuild, noVCS)
	checkRefused(t, "no vcs vs go install as base", noVCS, proxyBuild)
	code, out, _ := runCLI("--fail", "--trust-no-vcs", base, noVCS)
	if code != 1 || !strings.Contains(out, "Head has no VCS stamp, but base has one") {
		t.Fatalf("trusted: code %d\n%s", code, out)
	}
}

// constructed edits a real file in memory to make a case that the real
// examples don't cover. The result is labeled as made up in the test name.
func constructed(t *testing.T, path string, edits ...string) *Info {
	t.Helper()
	s := string(read(t, path))
	for i := 0; i < len(edits); i += 2 {
		if !strings.Contains(s, edits[i]) {
			t.Fatalf("%s has no %q", path, edits[i])
		}
		s = strings.Replace(s, edits[i], edits[i+1], 1)
	}
	in, err := ParseText("constructed", []byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func TestConstructedFindings(t *testing.T) {
	v141 := exDir + "/orders-v1.4.1.txt"
	ne := realDir + "/node_exporter-1.9.1-linux-arm64.txt"
	base := load(t, v141)
	cases := []struct {
		name  string
		base  *Info
		head  *Info
		kinds []string
	}{
		{"checksum", base, constructed(t, v141, "h1:zyQAAkrwaneQ066sspRyJaG9VNi/YJ1NfzcGB3hZ/qo=", "h1:AAAAAkrwaneQ066sspRyJaG9VNi/YJ1NfzcGB3hZ/qo="), []string{"checksum"}},
		{"replaced by module", base, constructed(t, v141, "\tdep\tgolang.org/x/text\tv0.21.0\th1:zyQAAkrwaneQ066sspRyJaG9VNi/YJ1NfzcGB3hZ/qo=\n",
			"\tdep\tgolang.org/x/text\tv0.21.0\n\t=>\texample.com/text-fork\tv0.21.1\th1:zyQAAkrwaneQ066sspRyJaG9VNi/YJ1NfzcGB3hZ/qo=\n"), []string{"replace"}},
		{"go downgrade", base, constructed(t, v141, "orders: go1.27.3", "orders: go1.27.1"), []string{"go-downgrade"}},
		{"race", base, constructed(t, v141, "\tbuild\t-tags=netgo\n", "\tbuild\t-race=true\n\tbuild\t-tags=netgo\n"), []string{"instrumented"}},
		{"debug gcflags", base, constructed(t, v141, "\tbuild\t-tags=netgo\n", "\tbuild\t-gcflags=\"all=-N -l\"\n\tbuild\t-tags=netgo\n"), []string{"debug-build"}},
		{"cpu level", load(t, realDir+"/node_exporter-1.9.1-linux-amd64.txt"), constructed(t, realDir+"/node_exporter-1.9.1-linux-amd64.txt", "GOAMD64=v1", "GOAMD64=v3"), []string{"cpu-level"}},
		{"older commit", base, constructed(t, v141, "vcs.time=2026-10-27T10:00:00Z", "vcs.time=2026-10-01T10:00:00Z"), []string{"older-commit"}},
		{"other program", base, constructed(t, v141, "\tpath\texample.com/orders\n", "\tpath\texample.com/orders/cmd/admin\n"), []string{"different-program"}},
		{"ldflags", load(t, ne), constructed(t, ne, "  -extldflags '-static'", ""), []string{"ldflags"}},
		{"buildmode", base, constructed(t, v141, "-buildmode=exe", "-buildmode=pie"), []string{"build-setting"}},
		{"replacement removed, head still dirty", load(t, exDir+"/orders-hotfix.txt"), constructed(t, exDir+"/orders-hotfix.txt", "\t=>\t../uuid-fork\t(devel)\t\n", ""), []string{"dirty"}},
	}
	for _, c := range cases {
		t.Run("constructed "+c.name, func(t *testing.T) {
			wantKinds(t, Compare(c.base, c.head), c.kinds...)
		})
	}
}

func TestBadStructureIsRefused(t *testing.T) {
	v141 := string(read(t, exDir+"/orders-v1.4.1.txt"))
	cases := map[string]string{
		"no newline":         strings.TrimSuffix(v141, "\n"),
		"duplicate dep":      strings.Replace(v141, "\tdep\tgolang.org/x/text", "\tdep\tgithub.com/google/uuid\tv1.6.0\n\tdep\tgolang.org/x/text", 1),
		"bad sum":            strings.Replace(v141, "h1:zyQ", "x1:zyQ", 1),
		"bad modified":       strings.Replace(v141, "vcs.modified=false", "vcs.modified=maybe", 1),
		"bad time":           strings.Replace(v141, "vcs.time=2026-10-27T10:00:00Z", "vcs.time=yesterday", 1),
		"no revision":        strings.Replace(v141, "\tbuild\tvcs.revision=", "\tbuild\tvcs.rev=", 1),
		"unknown line":       strings.Replace(v141, "\tpath\t", "\tpkg\t", 1),
		"unindented line":    strings.Replace(v141, "\tpath\t", "path\t", 1),
		"no GOARM64":         strings.Replace(v141, "\tbuild\tGOARM64=v8.0\n", "", 1),
		"old go no settings": "app: go1.17.13\n\tpath\texample.com/app\n\tmod\texample.com/app\t(devel)\t\n",
	}
	for name, s := range cases {
		if _, err := ParseText(name, []byte(s)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestVersionOrder(t *testing.T) {
	semver := [][2]string{
		{"v1.2.3", "v1.2.4"}, {"v1.2.3", "v1.10.0"}, {"v1.0.0-rc.1", "v1.0.0"}, {"v1.0.0-alpha", "v1.0.0-alpha.1"},
		{"v0.0.0-20240416160154-fe59bbe5cc7f", "v0.0.0-20240909161429-701f63a606c0"},
		{"v0.15.2-0.20240603130017-1754b780536b", "v0.15.2"}, {"v0.15.1", "v0.15.2-0.20240603130017-1754b780536b"},
		{"v2.0.0+incompatible", "v2.1.0+incompatible"}, {"v1.0.0-2", "v1.0.0-10"}, {"v1.0.0-9", "v1.0.0-a"},
	}
	for _, p := range semver {
		if c, ok := compareSemver(p[0], p[1]); !ok || c != -1 {
			t.Errorf("%s < %s: got %d %v", p[0], p[1], c, ok)
		}
		if c, ok := compareSemver(p[1], p[0]); !ok || c != 1 {
			t.Errorf("%s > %s: got %d %v", p[1], p[0], c, ok)
		}
	}
	// Same commit second, different hash: no order.
	if _, ok := compareSemver("v0.0.0-20261102203258-c514eb252d9e", "v0.0.0-20261102203258-8d8e85e6069e"); ok {
		t.Error("pseudo-versions from the same second compared")
	}
	for _, v := range []string{"(devel)", "1.2.3", "v1.2", "v1.2.3-", ""} {
		if _, ok := compareSemver(v, "v1.0.0"); ok {
			t.Errorf("%q compared", v)
		}
	}
	gov := [][2]string{{"go1.22.5", "go1.23.7"}, {"go1.22rc1", "go1.22.0"}, {"go1.22beta1", "go1.22rc1"}, {"go1.21", "go1.21.1"}, {"go1.22.5", "go1.22.10 X:boringcrypto"}}
	for _, p := range gov {
		if c, ok := compareGo(p[0], p[1]); !ok || c != -1 {
			t.Errorf("%s < %s: got %d %v", p[0], p[1], c, ok)
		}
	}
	if _, ok := compareGo("devel go1.24-abc", "go1.23.0"); ok {
		t.Error("devel compared")
	}
}

func TestLdflagsSplit(t *testing.T) {
	xs, keys, other := splitLdflags(`-s -w -X 'main.v=1 2' -X=main.b=x -extldflags '-static'`)
	if len(keys) != 2 || xs["main.v"] != "1 2" || xs["main.b"] != "x" || strings.Join(other, "|") != "-s|-w|-extldflags|-static" {
		t.Fatalf("%v %v %v", xs, keys, other)
	}
}

func TestUsage(t *testing.T) {
	good := exDir + "/orders-v1.4.1.txt"
	for _, args := range [][]string{{}, {good}, {good, good, good}, {"-", "-"}, {"--nope", good, good}} {
		if code, out, _ := runCLI(args...); code != 2 || out != "" {
			t.Errorf("%v: code %d", args, code)
		}
	}
	if code, out, _ := runCLI("--version"); code != 0 || out != "buildinfodiff "+version+"\n" {
		t.Errorf("version: %d %q", code, out)
	}
	// Flags may come after the inputs.
	if code, _, _ := runCLI(good, exDir+"/orders-hotfix.txt", "--fail"); code != 1 {
		t.Errorf("trailing --fail: code %d", code)
	}
	code, out, _ := runCLI("--json", good, exDir+"/orders-hotfix.txt")
	if code != 0 || !strings.Contains(out, `"kind": "dirty"`) {
		t.Errorf("json: %d\n%s", code, out)
	}
}
