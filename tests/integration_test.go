// Package tests runs buildinfodiff the way the skill and the README do: as a
// single Go file, on saved output and on real binaries built here.
package tests

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "buildinfodiff-bin")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "buildinfodiff")
	// Build the single file, as the skill runs it.
	out, err := exec.Command("go", "build", "-o", bin, "../plugin/scripts/buildinfodiff.go").CombinedOutput()
	if err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func buildinfodiff(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = ".."
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, out.String(), errOut.String()
}

// readmeExample is one `$ command` in the README and the output under it.
type readmeExample struct {
	line int
	cmd  string
	want string
}

func readmeExamples(t *testing.T) []readmeExample {
	f, err := os.Open("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	var examples []readmeExample
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "    $ ") {
			continue
		}
		ex := readmeExample{line: i + 1, cmd: strings.TrimPrefix(lines[i], "    $ ")}
		var out []string
		j := i + 1
		for ; j < len(lines); j++ {
			l := lines[j]
			if strings.HasPrefix(l, "    $ ") || (l != "" && !strings.HasPrefix(l, "    ")) {
				break
			}
			out = append(out, strings.TrimPrefix(l, "    "))
		}
		for len(out) > 0 && out[len(out)-1] == "" {
			out = out[:len(out)-1]
		}
		ex.want = strings.Join(out, "\n")
		examples = append(examples, ex)
		i = j - 1
	}
	return examples
}

// TestReadmeExamples runs every `$ ` line in the README from the repository
// root and checks that the output shown under it is what the command prints.
func TestReadmeExamples(t *testing.T) {
	examples := readmeExamples(t)
	if len(examples) < 4 {
		t.Fatalf("only %d README examples found", len(examples))
	}
	for _, ex := range examples {
		t.Run(fmt.Sprintf("line%d", ex.line), func(t *testing.T) {
			cmd := exec.Command("sh", "-c", ex.cmd)
			cmd.Dir = ".."
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("README line %d: %s: %v\n%s", ex.line, ex.cmd, err, out)
			}
			got := strings.TrimRight(string(out), "\n")
			if got != ex.want {
				t.Fatalf("README line %d: %s\n--- README\n%s\n--- actual\n%s", ex.line, ex.cmd, ex.want, got)
			}
		})
	}
}

// project is a throwaway git repository holding a small program and a local
// module it replaces, so builds need no network.
type project struct {
	t   *testing.T
	dir string
}

func newProject(t *testing.T) *project {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	p := &project{t: t, dir: filepath.Join(root, "svc")}
	files := map[string]string{
		"lib/go.mod":     "module example.com/lib\n\ngo 1.22\n",
		"lib/lib.go":     "package lib\n\nfunc Name() string { return \"svc\" }\n",
		"svc/go.mod":     "module example.com/svc\n\ngo 1.22\n\nrequire example.com/lib v1.0.0\n\nreplace example.com/lib => ../lib\n",
		"svc/main.go":    "package main\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/lib\"\n)\n\nfunc main() { fmt.Println(lib.Name()) }\n",
		"svc/.gitignore": "out/\n",
	}
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p.git("init", "-q")
	p.commit("first")
	return p
}

func (p *project) git(args ...string) {
	p.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	cmd.Dir = p.dir
	if out, err := cmd.CombinedOutput(); err != nil {
		p.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (p *project) commit(msg string) {
	p.t.Helper()
	p.git("add", "-A")
	p.git("commit", "-q", "-m", msg)
}

func (p *project) edit(line string) {
	p.t.Helper()
	f, err := os.OpenFile(filepath.Join(p.dir, "main.go"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		p.t.Fatal(err)
	}
	fmt.Fprintln(f, line)
	f.Close()
}

// build runs go build with the given environment and flags, offline.
func (p *project) build(name string, env []string, flags ...string) string {
	p.t.Helper()
	out := filepath.Join(p.dir, "out", name)
	cmd := exec.Command("go", append(append([]string{"build", "-o", out}, flags...), ".")...)
	cmd.Dir = p.dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off", "GOFLAGS=", "CGO_ENABLED=0")
	cmd.Env = append(cmd.Env, env...)
	if b, err := cmd.CombinedOutput(); err != nil {
		p.t.Fatalf("go build %v: %v\n%s", flags, err, b)
	}
	return out
}

// versionText saves `go version -m` output for a binary.
func versionText(t *testing.T, binary string, json bool) string {
	t.Helper()
	args := []string{"version", "-m"}
	if json {
		args = append(args, "-json")
	}
	out, err := exec.Command("go", append(args, binary)...).Output()
	if err != nil {
		t.Fatalf("go version -m %s: %v", binary, err)
	}
	ext := ".txt"
	if json {
		ext = ".json"
	}
	path := binary + ext
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func expect(t *testing.T, label string, wantCode int, wantText []string, args ...string) string {
	t.Helper()
	code, out, errOut := buildinfodiff(t, args...)
	if code != wantCode {
		t.Fatalf("%s: code %d, want %d\n%s%s", label, code, wantCode, out, errOut)
	}
	for _, w := range wantText {
		if !strings.Contains(out, w) {
			t.Fatalf("%s: missing %q\n%s", label, w, out)
		}
	}
	return out
}

func TestRealBinaries(t *testing.T) {
	p := newProject(t)
	rel1 := p.build("rel1", nil, "-trimpath")
	p.edit("// a comment-only change")
	p.commit("comment")
	rel2 := p.build("rel2", nil, "-trimpath")

	// A healthy release: only the revision changed. It must not say "No
	// differences", because the commits differ.
	out := expect(t, "healthy", 0, []string{"Nothing needs a look."}, "--fail", rel1, rel2)
	if strings.Contains(out, "No differences") {
		t.Fatalf("different commits reported as no differences:\n%s", out)
	}

	// The binary and its go version -m output, text and JSON, read the same.
	for _, json := range []bool{false, true} {
		if json && exec.Command("go", "version", "-m", "-json", rel2).Run() != nil {
			continue // older go commands have no -json flag
		}
		expect(t, "binary vs go version -m", 0, []string{"No differences"}, "--fail", rel2, versionText(t, rel2, json))
	}

	cases := []struct {
		name  string
		env   []string
		flags []string
		want  string
	}{
		{"tags", nil, []string{"-trimpath", "-tags", "extra"}, "Build tags changed: added extra"},
		{"no trimpath", nil, nil, "-trimpath was removed"},
		{"no vcs", nil, []string{"-trimpath", "-buildvcs=false"}, "Head has no VCS stamp, but base has one"},
		{"gcflags", nil, []string{"-trimpath", "-gcflags=all=-N -l"}, "which turns off optimizations or inlining"},
	}
	for _, c := range cases {
		head := p.build(c.name, c.env, c.flags...)
		expect(t, c.name, 1, []string{c.want}, "--fail", rel2, head)
	}

	// CPU level, cross-compiled.
	v1 := p.build("amd64-v1", []string{"GOOS=linux", "GOARCH=amd64", "GOAMD64=v1"}, "-trimpath")
	v3 := p.build("amd64-v3", []string{"GOOS=linux", "GOARCH=amd64", "GOAMD64=v3"}, "-trimpath")
	expect(t, "GOAMD64", 1, []string{"GOAMD64 changed from v1 to v3"}, "--fail", v1, v3)
	expect(t, "platform", 1, []string{"The platform differs"}, "--fail", rel2, v1)

	// Uncommitted change.
	p.edit("// not committed")
	dirty := p.build("dirty", nil, "-trimpath")
	expect(t, "dirty", 1, []string{"uncommitted changes (vcs.modified=true)"}, "--fail", rel2, dirty)
	expect(t, "dirty without --fail", 0, []string{"1 change needs a look"}, rel2, dirty)
}

func TestRaceBuild(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("no C compiler for -race")
	}
	p := newProject(t)
	rel := p.build("rel", nil, "-trimpath")
	race := p.build("race", []string{"CGO_ENABLED=1"}, "-trimpath", "-race")
	expect(t, "race", 1, []string{"Head was built with -race", "CGO_ENABLED changed from 0 to 1"}, "--fail", rel, race)
}

// TestCutBinariesAreNeverTrusted cuts real binaries for three formats at many
// points, on each side, with and without --fail.
func TestCutBinariesAreNeverTrusted(t *testing.T) {
	p := newProject(t)
	points := 60
	if testing.Short() {
		points = 12
	}
	for _, target := range []string{"linux/amd64", "linux/arm64", "windows/amd64", "darwin/arm64"} {
		goos, goarch, _ := strings.Cut(target, "/")
		whole := p.build(goos+"-"+goarch, []string{"GOOS=" + goos, "GOARCH=" + goarch}, "-trimpath")
		expect(t, target+" whole", 0, []string{"No differences"}, "--fail", whole, versionText(t, whole, false))
		data, err := os.ReadFile(whole)
		if err != nil {
			t.Fatal(err)
		}
		cut := whole + ".cut"
		sizes := []int{len(data) - 1, 1, 4, 64}
		for i := 1; i <= points; i++ {
			sizes = append(sizes, len(data)*i/(points+1))
		}
		for _, n := range sizes {
			if err := os.WriteFile(cut, data[:n], 0o644); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{whole, cut}, {cut, whole}, {"--fail", whole, cut}, {"--fail", cut, whole}} {
				code, out, errOut := buildinfodiff(t, args...)
				if code != 2 || out != "" || !strings.Contains(errOut, "can't be trusted") {
					t.Fatalf("%s cut to %d of %d bytes %v: code %d\n%s%s", target, n, len(data), args, code, out, errOut)
				}
			}
		}
	}
}

func TestNonGoBinaryIsRefused(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "hello.c")
	os.WriteFile(src, []byte("int main(void) { return 0; }\n"), 0o644)
	out := filepath.Join(dir, "hello")
	if b, err := exec.Command(cc, "-o", out, src).CombinedOutput(); err != nil {
		t.Skipf("cc failed: %v\n%s", err, b)
	}
	good := "examples/buildinfo/orders-v1.4.1.txt"
	for _, args := range [][]string{{good, out}, {out, good}, {"--fail", good, out}} {
		code, stdout, errOut := buildinfodiff(t, args...)
		if code != 2 || stdout != "" || !strings.Contains(errOut, "has no Go build information") {
			t.Fatalf("%v: code %d\n%s%s", args, code, stdout, errOut)
		}
	}
}
