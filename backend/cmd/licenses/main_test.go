package main

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func TestLicenseCollectionCannotOmitMissingOrEmptyNotices(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "NOTICE.txt"), []byte("upstream attribution\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readNotices(directory); err == nil {
		t.Fatal("NOTICE without LICENSE must fail")
	}
	license := []byte("copyright owner\nlicense terms\n")
	if err := os.WriteFile(filepath.Join(directory, "LICENSE"), license, 0600); err != nil {
		t.Fatal(err)
	}
	notices, err := readNotices(directory)
	if err != nil || len(notices) != 2 {
		t.Fatalf("full license and notice required: %v, %d", err, len(notices))
	}
	if string(notices[0].content) != string(license) || notices[0].SHA256 != digest(license) {
		t.Fatal("upstream bytes must be preserved")
	}
	if err := os.WriteFile(filepath.Join(directory, "NOTICE.txt"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readNotices(directory); err == nil {
		t.Fatal("empty NOTICE must fail rather than be omitted")
	}
}

func TestBinaryModulesMustMatchLockedReadableSources(t *testing.T) {
	dependency := &debug.Module{Path: "example.test/module", Version: "v1.0.0", Sum: "h1:locked"}
	listed := &listedModule{Path: dependency.Path, Version: dependency.Version, Sum: dependency.Sum, Dir: t.TempDir()}
	if err := verifyModule(dependency, listed); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*listedModule){
		func(item *listedModule) { item.Version = "v2.0.0" },
		func(item *listedModule) { item.Sum = "h1:different" },
		func(item *listedModule) { item.Dir = filepath.Join(t.TempDir(), "missing") },
		func(item *listedModule) { item.Replace = &listedModule{Path: "local"} },
	} {
		copy := *listed
		change(&copy)
		if err := verifyModule(dependency, &copy); err == nil {
			t.Fatal("mismatch must fail")
		}
	}
	if err := verifyModule(dependency, nil); err == nil {
		t.Fatal("unmapped dependency must fail")
	}
	dependency.Replace = &debug.Module{Path: "replacement"}
	if err := verifyModule(dependency, listed); err == nil {
		t.Fatal("binary replacements must fail")
	}
}

func TestCollectionNeverOverwritesAnExistingDirectory(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "keep.txt")
	if err := os.WriteFile(target, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	err := collect(directory, []string{"not-even-a-binary"})
	if err == nil || !strings.Contains(err.Error(), "must be empty") {
		t.Fatalf("must reject before reading binaries: %v", err)
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "preserve" {
		t.Fatal("existing content changed")
	}
}

func TestNestedLicensesKeepPathsAndCannotReplaceTheModuleLicense(t *testing.T) {
	directory := t.TempDir()
	nested := filepath.Join(directory, "internal", "sync", "singleflight")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	bsd := []byte("separate BSD copyright\r\ncomplete BSD terms\r\n")
	if err := os.WriteFile(filepath.Join(nested, "LICENSE"), bsd, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readNotices(directory); err == nil {
		t.Fatal("nested license must not hide missing module license")
	}
	if err := os.WriteFile(filepath.Join(directory, "LICENSE"), []byte("module Apache license\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "NOTICE"), []byte("nested upstream attribution\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"notice_response.go", "license-check.yml"} {
		if err := os.WriteFile(filepath.Join(nested, name), []byte("not a legal notice\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	notices, err := readNotices(directory)
	if err != nil || len(notices) != 3 {
		t.Fatalf("need root and both nested files: %v, %d", err, len(notices))
	}
	found := map[string]notice{}
	for _, item := range notices {
		found[item.Path] = item
	}
	if got := found["internal/sync/singleflight/LICENSE"]; string(got.content) != string(bsd) || got.SHA256 != digest(bsd) {
		t.Fatal("nested license bytes/path must be retained separately from root LICENSE")
	}
	if err := os.WriteFile(filepath.Join(nested, "NOTICE"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readNotices(directory); err == nil {
		t.Fatal("an empty nested NOTICE must fail rather than disappear")
	}
}

func TestModuleSymlinkIsRejectedRatherThanFollowedOrSkipped(t *testing.T) {
	directory := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "LICENSE"), []byte("module license\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "NOTICE"), []byte("must not follow\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "linked-notices")); err != nil {
		t.Skipf("creating symlinks is unavailable on this host: %v", err)
	}
	if _, err := readNotices(directory); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("module symlink must fail explicitly: %v", err)
	}
}
