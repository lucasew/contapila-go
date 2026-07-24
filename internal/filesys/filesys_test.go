package filesys

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestOverlayReadFile(t *testing.T) {
	o := NewOverlay(OS{})
	o.Set("/tmp/foo.beancount", "hello")
	b, err := o.ReadFile("/tmp/foo.beancount")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Fatalf("%q", b)
	}
}

// stubFS is an in-memory base used to assert fallthrough without touching the real disk.
type stubFS struct {
	files map[string]string
	dirs  map[string][]fs.DirEntry
	err   error
}

func (s stubFS) ReadFile(path string) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	if v, ok := s.files[path]; ok {
		return []byte(v), nil
	}
	return nil, fs.ErrNotExist
}

func (s stubFS) Stat(path string) (fs.FileInfo, error) {
	if s.err != nil {
		return nil, s.err
	}
	if v, ok := s.files[path]; ok {
		return overlayInfo{name: filepath.Base(path), size: int64(len(v))}, nil
	}
	return nil, fs.ErrNotExist
}

func (s stubFS) ReadDir(path string) ([]fs.DirEntry, error) {
	if s.err != nil {
		return nil, s.err
	}
	if d, ok := s.dirs[path]; ok {
		return d, nil
	}
	return nil, fs.ErrNotExist
}

func TestNewOverlayNilBaseUsesOS(t *testing.T) {
	o := NewOverlay(nil)
	if _, ok := o.Base.(OS); !ok {
		t.Fatalf("Base type=%T; want OS", o.Base)
	}
}

func TestOverlayGetSetDelete(t *testing.T) {
	o := NewOverlay(stubFS{})
	path := filepath.Join(t.TempDir(), "a.beancount")

	if _, ok := o.Get(path); ok {
		t.Fatal("Get on empty overlay should miss")
	}

	o.Set(path, "body")
	got, ok := o.Get(path)
	if !ok || got != "body" {
		t.Fatalf("Get after Set: ok=%v got=%q", ok, got)
	}

	// Cleaned equivalent paths share one overlay entry.
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	dirty := filepath.Join(filepath.Dir(abs), ".", filepath.Base(abs))
	if g, ok := o.Get(dirty); !ok || g != "body" {
		t.Fatalf("Get via dirty path %q: ok=%v got=%q", dirty, ok, g)
	}

	o.Delete(path)
	if _, ok := o.Get(path); ok {
		t.Fatal("Get after Delete should miss")
	}
}

func TestOverlayPaths(t *testing.T) {
	o := NewOverlay(stubFS{})
	dir := t.TempDir()
	a := filepath.Join(dir, "a.beancount")
	b := filepath.Join(dir, "b.beancount")
	o.Set(a, "1")
	o.Set(b, "2")

	paths := o.Paths()
	if len(paths) != 2 {
		t.Fatalf("Paths=%v; want 2 entries", paths)
	}
	// Paths are cleaned absolute.
	sort.Strings(paths)
	wantA, _ := filepath.Abs(a)
	wantB, _ := filepath.Abs(b)
	want := []string{wantA, wantB}
	sort.Strings(want)
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("Paths[%d]=%q want %q (all=%v)", i, paths[i], want[i], paths)
		}
	}
}

func TestOverlayReadFileFallsThrough(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "on-disk.beancount")
	abs, err := filepath.Abs(basePath)
	if err != nil {
		t.Fatal(err)
	}
	base := stubFS{files: map[string]string{abs: "from-base"}}
	o := NewOverlay(base)

	// No overlay → base content.
	b, err := o.ReadFile(basePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "from-base" {
		t.Fatalf("fallthrough=%q", b)
	}

	// Overlay wins over base.
	o.Set(basePath, "from-overlay")
	b, err = o.ReadFile(basePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "from-overlay" {
		t.Fatalf("overlay=%q", b)
	}

	// After delete, fall through again.
	o.Delete(basePath)
	b, err = o.ReadFile(basePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "from-base" {
		t.Fatalf("after delete=%q", b)
	}
}

func TestOverlayStat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stat.beancount")
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	base := stubFS{files: map[string]string{abs: "xxxx"}} // size 4
	o := NewOverlay(base)

	// Base size when not overlayed.
	fi, err := o.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != 4 {
		t.Fatalf("base size=%d", fi.Size())
	}

	// Overlay size reflects buffer text, not base.
	o.Set(path, "longer-buffer")
	fi, err = o.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != int64(len("longer-buffer")) {
		t.Fatalf("overlay size=%d", fi.Size())
	}
	if fi.Name() != "stat.beancount" {
		t.Fatalf("name=%q", fi.Name())
	}
	if fi.IsDir() {
		t.Fatal("overlay Stat should not report IsDir")
	}
	if fi.Mode() != 0o644 {
		t.Fatalf("mode=%v", fi.Mode())
	}
	if fi.Sys() != nil {
		t.Fatalf("Sys=%v", fi.Sys())
	}
	if fi.ModTime().IsZero() {
		t.Fatal("ModTime zero")
	}
}

func TestOverlayReadDirDelegates(t *testing.T) {
	dir := t.TempDir()
	// Real OS ReadDir for a non-empty temp dir.
	if err := os.WriteFile(filepath.Join(dir, "x"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	o := NewOverlay(OS{})
	// Overlay content must not invent dir entries — ReadDir always hits base.
	o.Set(filepath.Join(dir, "ghost"), "not-on-disk")
	ents, err := o.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range ents {
		names[e.Name()] = true
	}
	if !names["x"] {
		t.Fatalf("missing on-disk entry; names=%v", names)
	}
	if names["ghost"] {
		t.Fatalf("overlay-only path leaked into ReadDir: %v", names)
	}
}

func TestOverlayReadFileMissing(t *testing.T) {
	base := stubFS{err: errors.New("base boom")}
	o := NewOverlay(base)
	// Missing overlay + base error propagates.
	if _, err := o.ReadFile("/no/such/file"); err == nil {
		t.Fatal("expected error")
	}
}
