package datadir

import (
	"os"
	"path/filepath"
	"testing"

	"q3vigilai/internal/store"
)

func TestDBPathMovesTheDatabaseOfTheFormerName(t *testing.T) {
	dir := t.TempDir()
	// A real database under the old name, with content to carry over.
	old := filepath.Join(dir, "q3vnlaw.db")
	s, err := store.Open(old)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.SaveTopic(store.Topic{Name: "Chủ đề cũ", Keywords: []string{"thuế"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	got := DBPath(dir)
	if filepath.Base(got) != DBName {
		t.Fatalf("path: %s", got)
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("the old file is still there next to the new one")
	}
	s2, err := store.Open(got)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	tp, err := s2.Topic(id)
	if err != nil || tp.Name != "Chủ đề cũ" {
		t.Errorf("the topic did not survive the rename: %+v %v", tp, err)
	}
	// Idempotent.
	if again := DBPath(dir); again != got {
		t.Errorf("second call: %s", again)
	}
}

func TestDBPathWhenNothingExistsOrBothExist(t *testing.T) {
	dir := t.TempDir()
	if got := DBPath(dir); filepath.Base(got) != DBName {
		t.Errorf("fresh folder: %s", got)
	}
	// Both present: the new one wins and the old one is left alone.
	os.WriteFile(filepath.Join(dir, "q3vigilai.db"), []byte("new"), 0o644)
	os.WriteFile(filepath.Join(dir, "q3vnlaw.db"), []byte("old"), 0o644)
	if got := DBPath(dir); filepath.Base(got) != DBName {
		t.Errorf("both present: %s", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "q3vnlaw.db")); string(b) != "old" {
		t.Error("the old database was touched")
	}
}

func TestDBPathKeepsTheOldFileWhenItCannotBeMoved(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "q3vnlaw.db")
	os.WriteFile(old, []byte("data"), 0o644)
	// The old version of the app still has the file open: on Windows it cannot be renamed.
	f, err := os.Open(old)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got := DBPath(dir)
	if filepath.Base(got) != "q3vnlaw.db" {
		t.Errorf("a database that cannot be moved must keep its name, got %s", got)
	}
}

func TestResolve(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "app", "q3vigilai.exe")
	os.MkdirAll(filepath.Dir(exe), 0o755)

	if dir, fb := Resolve("", exe); dir != filepath.Join(root, "app", "data") || fb {
		t.Errorf("portable folder: %s %v", dir, fb)
	}
	explicit := filepath.Join(root, "mine")
	if dir, fb := Resolve(explicit, exe); dir != explicit || fb {
		t.Errorf("explicit folder: %s %v", dir, fb)
	}
	if _, err := os.Stat(explicit); err != nil {
		t.Error("the explicit folder was not created")
	}

	// Next to the exe is a file called "data": not writable. The fallback is
	// LocalAppData, and data left by the former name is reused.
	exe2 := filepath.Join(root, "ro", "q3vigilai.exe")
	os.MkdirAll(filepath.Dir(exe2), 0o755)
	os.WriteFile(filepath.Join(root, "ro", "data"), []byte("x"), 0o644)
	local := filepath.Join(root, "local")
	t.Setenv("LOCALAPPDATA", local)
	if dir, fb := Resolve("", exe2); dir != filepath.Join(local, "Q3VigilAI") || !fb {
		t.Errorf("fallback: %s %v", dir, fb)
	}
	os.RemoveAll(filepath.Join(local, "Q3VigilAI"))
	os.MkdirAll(filepath.Join(local, "Q3VNLaw"), 0o755)
	if dir, _ := Resolve("", exe2); dir != filepath.Join(local, "Q3VNLaw") {
		t.Errorf("data of the former name was not reused: %s", dir)
	}
}
