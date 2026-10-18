// Command licenses packages upstream notices for the actual release binaries.
// Run from the backend module after building both binaries with this toolchain.
package main

import (
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"unicode/utf8"
)

type paths []string

func (p *paths) String() string         { return strings.Join(*p, ", ") }
func (p *paths) Set(value string) error { *p = append(*p, value); return nil }

type notice struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	content []byte
}
type module struct {
	Path    string   `json:"path"`
	Version string   `json:"version"`
	Sum     string   `json:"sum"`
	Notices []notice `json:"notices"`
}
type binary struct {
	Name      string   `json:"name"`
	Package   string   `json:"package"`
	SHA256    string   `json:"sha256"`
	GoVersion string   `json:"go_version"`
	Modules   []string `json:"modules"`
}
type manifest struct {
	Binaries  []binary `json:"binaries"`
	Modules   []module `json:"modules"`
	GoRuntime struct {
		Version string   `json:"version"`
		Notices []notice `json:"notices"`
	} `json:"go_runtime"`
}
type listedModule struct {
	Path, Version, Sum, Dir string
	Replace                 *listedModule
}

func main() {
	var binaries paths
	out := flag.String("out", "", "new or empty output directory")
	flag.Var(&binaries, "binary", "release binary; repeat for every binary")
	flag.Parse()
	if err := collect(*out, binaries); err != nil {
		fmt.Fprintln(os.Stderr, "license collection failed:", err)
		os.Exit(1)
	}
}

func collect(out string, filenames []string) error {
	if out == "" || len(filenames) == 0 || flag.NArg() != 0 {
		return errors.New("provide -out and at least one -binary")
	}
	if info, err := os.Lstat(out); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("output directory must not be a symlink")
	}
	if entries, err := os.ReadDir(out); err == nil {
		if len(entries) != 0 {
			return errors.New("output directory must be empty")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listed, err := listModules()
	if err != nil {
		return err
	}
	var result manifest
	dependencies := map[string]*debug.Module{}
	names := map[string]bool{}
	for _, filename := range filenames {
		info, err := buildinfo.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("read binary %s: %w", filename, err)
		}
		if info.GoVersion != runtime.Version() {
			return fmt.Errorf("binary %s used %s, collector used %s", filename, info.GoVersion, runtime.Version())
		}
		name := filepath.Base(filename)
		if names[name] {
			return fmt.Errorf("duplicate binary name %s", name)
		}
		names[name] = true
		content, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		item := binary{Name: name, Package: info.Path, SHA256: digest(content), GoVersion: info.GoVersion, Modules: []string{}}
		for _, dependency := range info.Deps {
			if err := verifyModule(dependency, listed[dependency.Path]); err != nil {
				return err
			}
			key := dependency.Path + "@" + dependency.Version
			dependencies[key] = dependency
			item.Modules = append(item.Modules, key)
		}
		sort.Strings(item.Modules)
		result.Binaries = append(result.Binaries, item)
	}
	sort.Slice(result.Binaries, func(i, j int) bool { return result.Binaries[i].Name < result.Binaries[j].Name })
	keys := make([]string, 0, len(dependencies))
	for key := range dependencies {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for index, key := range keys {
		dependency := dependencies[key]
		notices, err := readNotices(listed[dependency.Path].Dir)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		for i := range notices {
			notices[i].Path = fmt.Sprintf("modules/%03d/%s", index+1, notices[i].Path)
		}
		result.Modules = append(result.Modules, module{Path: dependency.Path, Version: dependency.Version, Sum: dependency.Sum, Notices: notices})
	}
	goLicense, err := readNotice(filepath.Join(runtime.GOROOT(), "LICENSE"), "go/LICENSE")
	if err != nil {
		return fmt.Errorf("Go runtime license: %w", err)
	}
	result.GoRuntime.Version = runtime.Version()
	result.GoRuntime.Notices = []notice{goLicense}
	return writeCollection(out, result)
}

func listModules() (map[string]*listedModule, error) {
	output, err := exec.Command("go", "list", "-m", "-mod=readonly", "-json", "all").Output()
	if err != nil {
		return nil, fmt.Errorf("list locked Go modules: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	modules := map[string]*listedModule{}
	for {
		var item listedModule
		if err := decoder.Decode(&item); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
		if modules[item.Path] != nil {
			return nil, fmt.Errorf("duplicate module %s", item.Path)
		}
		modules[item.Path] = &item
	}
	return modules, nil
}

func verifyModule(dependency *debug.Module, listed *listedModule) error {
	if dependency.Replace != nil || (listed != nil && listed.Replace != nil) {
		return fmt.Errorf("module replacements require an explicit license review: %s", dependency.Path)
	}
	if listed == nil || listed.Version != dependency.Version || listed.Sum != dependency.Sum || listed.Dir == "" {
		return fmt.Errorf("binary dependency is not available at its locked version: %s@%s", dependency.Path, dependency.Version)
	}
	info, err := os.Stat(listed.Dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("module directory is unavailable: %s", dependency.Path)
	}
	return nil
}

func noticeKind(name string) (selected, license bool) {
	name = strings.ToUpper(name)
	// Files such as notice_response.go and license-check.yml are application
	// sources/configuration, not upstream legal documents.
	extension := filepath.Ext(name)
	if extension != "" && extension != ".TXT" && extension != ".MD" && extension != ".RST" {
		return false, false
	}
	for _, prefix := range []string{"LICENSE", "LICENCE", "COPYING", "NOTICE", "AUTHORS", "COPYRIGHT", "PATENTS"} {
		if name == prefix || strings.HasPrefix(name, prefix+".") || strings.HasPrefix(name, prefix+"-") || strings.HasPrefix(name, prefix+"_") {
			return true, prefix == "LICENSE" || prefix == "LICENCE" || prefix == "COPYING"
		}
	}
	return false, false
}

func readNotices(directory string) ([]notice, error) {
	var notices []notice
	foundRootLicense := false
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("module contains a symlink; collection refused: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		selected, license := noticeKind(entry.Name())
		if !selected {
			return nil
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		item, err := readNotice(path, filepath.ToSlash(relative))
		if err != nil {
			return err
		}
		foundRootLicense = foundRootLicense || (license && filepath.Dir(relative) == ".")
		notices = append(notices, item)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !foundRootLicense {
		return nil, errors.New("no module-root license file; collection refused")
	}
	return notices, nil
}

func readNotice(source, name string) (notice, error) {
	info, err := os.Lstat(source)
	if err != nil {
		return notice{}, err
	}
	if !info.Mode().IsRegular() {
		return notice{}, fmt.Errorf("notice is not a regular file: %s", source)
	}
	content, err := os.ReadFile(source)
	if err != nil {
		return notice{}, err
	}
	if !utf8.Valid(content) || strings.TrimSpace(string(content)) == "" {
		return notice{}, fmt.Errorf("notice is empty or not UTF-8: %s", source)
	}
	return notice{Path: name, SHA256: digest(content), content: content}, nil
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func writeCollection(out string, result manifest) error {
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	write := func(name string, content []byte) error {
		target := filepath.Join(out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		_, err = file.Write(content)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	}
	for _, item := range result.Modules {
		for _, notice := range item.Notices {
			if err := write(notice.Path, notice.content); err != nil {
				return err
			}
		}
	}
	for _, notice := range result.GoRuntime.Notices {
		if err := write(notice.Path, notice.content); err != nil {
			return err
		}
	}
	content, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err := write("manifest.json", append(content, '\n')); err != nil {
		return err
	}
	return write("README.txt", []byte("Go release dependency licenses\n\nmanifest.json binds each binary's SHA-256 and Go version to its actual embedded dependency modules.\nModule license/notice bytes are copied unchanged from their locked module directories.\nThe go directory contains the matching build toolchain's runtime license.\nOS package copyright and license materials remain under /usr/share/doc in the Debian image.\nBrowser notices are served separately at /third-party-notices.txt.\n"))
}
