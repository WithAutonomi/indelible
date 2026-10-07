// Command notices writes THIRD-PARTY-NOTICES.txt for the indelible binary.
//
// It lists every module linked into ./cmd/indelible for the release targets
// (as resolved by `go list -deps` with CGO disabled, exactly like the release
// build), reproduces each module's licence and notice files, adds the Go
// standard library licence, the embedded Swagger UI notices and, when given,
// the notices for the npm packages bundled into the embedded web UI.
//
// Usage (from the repository root):
//
//	go run ./scripts/notices -web web/THIRD-PARTY-NOTICES.web.txt -o THIRD-PARTY-NOTICES.txt
//
// Release builds also pass -source (the exact commit) and -antd (the antd
// release bundled alongside).
//
// It fails when a linked module ships no licence file, or when a module other
// than the ones handled below carries GNU GPL-family licence text, so that a
// dependency change cannot silently leave the notices incomplete.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultTargets = "linux/amd64,linux/arm64,darwin/arm64,windows/amd64"

	goEthereumModule = "github.com/ethereum/go-ethereum"
	// antdGoModule is published from a subdirectory of the ant-sdk repository,
	// so its module zip carries no licence file; ant-sdk is MIT OR Apache-2.0.
	antdGoModule     = "github.com/WithAutonomi/ant-sdk/antd-go"
	swaggerUIModule  = "github.com/swaggo/files"
	swaggerUINotices = "third_party/swagger-ui/NOTICES.txt"

	sourceRepo = "https://github.com/WithAutonomi/indelible"
	rule       = "================================================================================"
	thinRule   = "--------------------------------------------------------------------------------"
)

// gplAllowed lists modules whose GNU GPL-family licence text is expected and
// explained in the notices. Any other module carrying such text stops the run.
var gplAllowed = map[string]bool{goEthereumModule: true}

// licenceFilePrefixes are the upper-cased file-name prefixes treated as licence
// or notice files.
var licenceFilePrefixes = []string{"LICENSE", "LICENCE", "COPYING", "COPYRIGHT", "NOTICE", "PATENTS", "UNLICENSE"}

// codeExtensions keep source files such as license.go or notice_test.go from
// being mistaken for licence files.
var codeExtensions = map[string]bool{
	".go": true, ".s": true, ".c": true, ".h": true, ".js": true, ".ts": true,
	".json": true, ".yml": true, ".yaml": true, ".proto": true, ".py": true,
	".sh": true, ".html": true, ".css": true, ".tmpl": true,
}

type goModule struct {
	Path    string
	Version string
	Dir     string
	Main    bool
	Replace *goModule
}

type goPackage struct {
	ImportPath string
	Dir        string
	Standard   bool
	Module     *goModule
	Error      *struct{ Err string }
}

type buildInfo struct {
	source string // exact commit or tag the binary was built from, if known
	antd   string // antd release bundled alongside, if known
}

type moduleInfo struct {
	path    string
	version string
	dir     string
	pkgDirs map[string]bool
	targets map[string]bool
}

func main() {
	targetsFlag := flag.String("targets", defaultTargets, "comma-separated GOOS/GOARCH release targets")
	pkg := flag.String("pkg", "./cmd/indelible", "main package whose linked modules are listed")
	webNotices := flag.String("web", "", "notices file for the bundled web UI (from web/scripts/third-party-notices.mjs)")
	source := flag.String("source", "", "commit or tag the binary is built from")
	antd := flag.String("antd", "", "antd release or image bundled alongside the binary")
	out := flag.String("o", "", "output file (default: stdout)")
	flag.Parse()

	info := buildInfo{source: *source, antd: *antd}
	if err := run(strings.Split(*targetsFlag, ","), *pkg, *webNotices, *out, info); err != nil {
		fmt.Fprintln(os.Stderr, "notices:", err)
		os.Exit(1)
	}
}

func run(targets []string, pkg, webNotices, out string, info buildInfo) error {
	modules := map[string]*moduleInfo{}
	for _, target := range targets {
		if err := collect(modules, strings.TrimSpace(target), pkg); err != nil {
			return err
		}
	}

	keys := make([]string, 0, len(modules))
	for k := range modules {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	files := map[string][]string{}
	var missing, unexpectedGPL []string
	for _, k := range keys {
		m := modules[k]
		found, err := licenceFiles(m)
		if err != nil {
			return err
		}
		if len(found) == 0 {
			if m.path != antdGoModule {
				missing = append(missing, k)
			}
			continue
		}
		files[k] = found
		if !gplAllowed[m.path] {
			gpl, err := containsGPLText(found)
			if err != nil {
				return err
			}
			if gpl {
				unexpectedGPL = append(unexpectedGPL, k)
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("no licence file found for: %s", strings.Join(missing, ", "))
	}
	if len(unexpectedGPL) > 0 {
		return fmt.Errorf("GNU GPL-family licence text found in %s; review the licence and extend this tool before shipping", strings.Join(unexpectedGPL, ", "))
	}

	var buf bytes.Buffer
	if err := render(&buf, targets, modules, keys, files, webNotices, info); err != nil {
		return err
	}
	if out == "" {
		_, err := os.Stdout.Write(buf.Bytes())
		return err
	}
	return os.WriteFile(out, buf.Bytes(), 0o644)
}

// collect adds the modules linked into pkg for one GOOS/GOARCH target.
func collect(modules map[string]*moduleInfo, target, pkg string) error {
	goos, goarch, ok := strings.Cut(target, "/")
	if !ok || goos == "" || goarch == "" {
		return fmt.Errorf("invalid target %q, want GOOS/GOARCH", target)
	}
	cmd := exec.Command("go", "list", "-deps", "-json", pkg)
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("go list for %s: %w: %s", target, err, strings.TrimSpace(stderr.String()))
	}

	dec := json.NewDecoder(bytes.NewReader(stdout))
	for {
		var p goPackage
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return fmt.Errorf("decode go list output for %s: %w", target, err)
		}
		if p.Error != nil {
			return fmt.Errorf("go list for %s: %s: %s", target, p.ImportPath, p.Error.Err)
		}
		if p.Standard || p.Module == nil || p.Module.Main {
			continue
		}
		mod := p.Module
		if mod.Replace != nil {
			mod = mod.Replace
		}
		key := mod.Path + "@" + mod.Version
		m, ok := modules[key]
		if !ok {
			m = &moduleInfo{path: mod.Path, version: mod.Version, dir: mod.Dir, pkgDirs: map[string]bool{}, targets: map[string]bool{}}
			modules[key] = m
		}
		m.pkgDirs[p.Dir] = true
		m.targets[target] = true
	}
}

// licenceFiles returns the licence and notice files of a module: those in the
// module root plus any found between a linked package's directory and the
// root, which covers vendored code that keeps its own licence in a
// subdirectory.
func licenceFiles(m *moduleInfo) ([]string, error) {
	if m.dir == "" {
		return nil, fmt.Errorf("module %s@%s has no directory; run go mod download", m.path, m.version)
	}
	seen := map[string]bool{}
	var found []string
	addDir := func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.IsDir() || !isLicenceFile(e.Name()) {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if !seen[p] {
				seen[p] = true
				found = append(found, p)
			}
		}
		return nil
	}

	if err := addDir(m.dir); err != nil {
		return nil, err
	}
	pkgDirs := make([]string, 0, len(m.pkgDirs))
	for d := range m.pkgDirs {
		pkgDirs = append(pkgDirs, d)
	}
	sort.Strings(pkgDirs)
	for _, d := range pkgDirs {
		for dir := d; dir != m.dir; dir = filepath.Dir(dir) {
			rel, err := filepath.Rel(m.dir, dir)
			if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
				break
			}
			if err := addDir(dir); err != nil {
				return nil, err
			}
		}
	}
	sort.Slice(found, func(i, j int) bool {
		di, dj := strings.Count(found[i], string(filepath.Separator)), strings.Count(found[j], string(filepath.Separator))
		if di != dj {
			return di < dj
		}
		return found[i] < found[j]
	})
	return found, nil
}

func isLicenceFile(name string) bool {
	if codeExtensions[strings.ToLower(filepath.Ext(name))] {
		return false
	}
	upper := strings.ToUpper(name)
	for _, prefix := range licenceFilePrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

func containsGPLText(paths []string) (bool, error) {
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return false, err
		}
		upper := bytes.ToUpper(data)
		if bytes.Contains(upper, []byte("GNU GENERAL PUBLIC LICENSE")) ||
			bytes.Contains(upper, []byte("GNU LESSER GENERAL PUBLIC LICENSE")) ||
			bytes.Contains(upper, []byte("GNU AFFERO GENERAL PUBLIC LICENSE")) {
			return true, nil
		}
	}
	return false, nil
}

func render(w *bytes.Buffer, targets []string, modules map[string]*moduleInfo, keys []string, files map[string][]string, webNotices string, info buildInfo) error {
	sourceURL := sourceRepo
	if info.source != "" {
		sourceURL = sourceRepo + "/tree/" + info.source
	}
	fmt.Fprintln(w, "THIRD-PARTY NOTICES FOR INDELIBLE")
	fmt.Fprintln(w, rule)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Indelible is licensed under either of the MIT License (LICENSE-MIT) or the")
	fmt.Fprintln(w, "Apache License, Version 2.0 (LICENSE-APACHE), at your option. The source code")
	fmt.Fprintf(w, "of this build is published at %s.\n", sourceURL)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "The indelible binary also contains the third-party software listed below. Each")
	fmt.Fprintln(w, "component is reproduced with its own licence and notice files. The list covers")
	fmt.Fprintf(w, "the release targets %s.\n", strings.Join(targets, ", "))
	fmt.Fprintln(w)

	if m := findModule(modules, keys, goEthereumModule); m != nil {
		fmt.Fprintln(w, rule)
		fmt.Fprintln(w, "GNU LESSER GENERAL PUBLIC LICENSE COMPONENT: go-ethereum")
		fmt.Fprintln(w, rule)
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Indelible contains library code from go-ethereum (%s %s),\n", m.path, m.version)
		fmt.Fprintln(w, "which is licensed under the GNU Lesser General Public License, version 3 or (at")
		fmt.Fprintln(w, "your option) any later version. The GNU LGPL v3 (COPYING.LESSER) and the GNU")
		fmt.Fprintln(w, "GPL v3 (COPYING) are reproduced in full in the go-ethereum section below.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "The complete source code of that exact go-ethereum version is available at:")
		fmt.Fprintf(w, "  %s\n", proxyURL(m.path, m.version))
		fmt.Fprintf(w, "  https://github.com/ethereum/go-ethereum/tree/%s\n", m.version)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "The source code of this Indelible build is published under MIT OR Apache-2.0")
		fmt.Fprintf(w, "at %s. To rebuild it against a modified go-ethereum:\n", sourceURL)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  1. check out that source and build the web interface:")
		fmt.Fprintln(w, "       cd web && npm ci && npm run build && cd ..")
		fmt.Fprintln(w, "  2. point go.mod at the modified copy:")
		fmt.Fprintf(w, "       go mod edit -replace %s=<path to the modified copy>\n", goEthereumModule)
		fmt.Fprintln(w, "  3. build with the release settings, choosing GOOS and GOARCH for the target:")
		fmt.Fprintln(w, "       CGO_ENABLED=0 go build -o indelible ./cmd/indelible")
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, rule)
	fmt.Fprintln(w, "BUNDLED SEPARATE PROGRAMS")
	fmt.Fprintln(w, rule)
	fmt.Fprintln(w)
	antd := "the antd daemon"
	if info.antd != "" {
		antd = fmt.Sprintf("the antd daemon (%s)", info.antd)
	}
	fmt.Fprintf(w, "Indelible release assets and Docker images also include %s\n", antd)
	fmt.Fprintln(w, "from https://github.com/WithAutonomi/ant-sdk. antd is a separate program that")
	fmt.Fprintln(w, "Indelible starts as a child process or reaches over HTTP; it is not linked")
	fmt.Fprintln(w, "into the indelible binary. Its licence and third-party notices are those of the")
	fmt.Fprintln(w, "ant-sdk release it comes from.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "The Docker image is based on debian:trixie-slim. The licence of each Debian")
	fmt.Fprintln(w, "package in the image is installed under /usr/share/doc/<package>/copyright, and")
	fmt.Fprintln(w, "the corresponding source packages are available from https://sources.debian.org/")
	fmt.Fprintln(w, "and https://snapshot.debian.org/.")
	fmt.Fprintln(w)

	if err := renderStdlib(w); err != nil {
		return err
	}

	fmt.Fprintln(w, rule)
	fmt.Fprintf(w, "GO MODULES (%d)\n", len(keys))
	fmt.Fprintln(w, rule)
	for _, k := range keys {
		m := modules[k]
		fmt.Fprintln(w)
		fmt.Fprintln(w, thinRule)
		fmt.Fprintf(w, "%s %s\n", m.path, m.version)
		fmt.Fprintf(w, "Linked on: %s\n", targetList(m, targets))
		fmt.Fprintf(w, "Source: %s\n", proxyURL(m.path, m.version))
		fmt.Fprintln(w, thinRule)
		if len(files[k]) == 0 {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Published from the WithAutonomi/ant-sdk repository, which is licensed under")
			fmt.Fprintln(w, "MIT OR Apache-2.0 (the same licence files as Indelible's: LICENSE-MIT and")
			fmt.Fprintln(w, "LICENSE-APACHE).")
		}
		for _, f := range files[k] {
			rel, err := filepath.Rel(m.dir, f)
			if err != nil {
				rel = filepath.Base(f)
			}
			if err := appendFile(w, filepath.ToSlash(rel), f); err != nil {
				return err
			}
		}
	}

	if findModule(modules, keys, swaggerUIModule) != nil {
		fmt.Fprintln(w)
		fmt.Fprintln(w, rule)
		fmt.Fprintf(w, "SWAGGER UI (embedded by %s)\n", swaggerUIModule)
		fmt.Fprintln(w, rule)
		if err := appendFile(w, "", swaggerUINotices); err != nil {
			return err
		}
	}

	if webNotices != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, rule)
		fmt.Fprintln(w, "WEB UI (npm packages bundled into the embedded web interface)")
		fmt.Fprintln(w, rule)
		if err := appendFile(w, "", webNotices); err != nil {
			return err
		}
	}
	return nil
}

func renderStdlib(w *bytes.Buffer) error {
	goroot, err := goEnv("GOROOT")
	if err != nil {
		return err
	}
	goversion, err := goEnv("GOVERSION")
	if err != nil {
		return err
	}
	fmt.Fprintln(w, rule)
	fmt.Fprintf(w, "GO STANDARD LIBRARY AND RUNTIME (%s)\n", goversion)
	fmt.Fprintln(w, rule)
	fmt.Fprintf(w, "Source: https://go.dev/dl/ (%s)\n", goversion)
	for _, name := range []string{"LICENSE", "PATENTS"} {
		if err := appendFile(w, name, filepath.Join(goroot, name)); err != nil {
			return err
		}
	}
	fmt.Fprintln(w)
	return nil
}

func goEnv(key string) (string, error) {
	out, err := exec.Command("go", "env", key).Output()
	if err != nil {
		return "", fmt.Errorf("go env %s: %w", key, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func appendFile(w *bytes.Buffer, label, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fmt.Fprintln(w)
	if label != "" {
		fmt.Fprintf(w, "--- %s ---\n\n", label)
	}
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	w.Write(bytes.TrimRight(data, "\n"))
	fmt.Fprintln(w)
	return nil
}

func findModule(modules map[string]*moduleInfo, keys []string, path string) *moduleInfo {
	for _, k := range keys {
		if modules[k].path == path {
			return modules[k]
		}
	}
	return nil
}

func targetList(m *moduleInfo, targets []string) string {
	if len(m.targets) == len(targets) {
		return "all release targets"
	}
	list := make([]string, 0, len(m.targets))
	for t := range m.targets {
		list = append(list, t)
	}
	sort.Strings(list)
	return strings.Join(list, ", ")
}

// proxyURL is the Go module proxy download URL of a module version, with the
// case-encoding the proxy protocol requires (an upper-case letter becomes '!'
// followed by its lower-case form).
func proxyURL(path, version string) string {
	return "https://proxy.golang.org/" + escapeModule(path) + "/@v/" + escapeModule(version) + ".zip"
}

func escapeModule(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('!')
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
