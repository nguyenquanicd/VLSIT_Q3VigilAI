// Package datadir decides where the app keeps its data and carries data over
// from the app's former name. The app was called Q3VNLaw before it was called
// Q3VigilAI; a user who upgrades keeps their topics, alerts and documents.
package datadir

import (
	"log"
	"os"
	"path/filepath"
)

const (
	// DBName is the database file.
	DBName = "q3vigilai.db"

	appDirName       = "Q3VigilAI"
	legacyAppDirName = "Q3VNLaw"
	legacyDBName     = "q3vnlaw.db"
)

// Resolve prefers "data" next to the executable, which is what makes the app
// portable. If that is not writable it falls back to the user's local
// application data (fallback is then true). An explicit folder always wins.
func Resolve(flagValue, exe string) (dir string, fallback bool) {
	if flagValue != "" {
		abs, _ := filepath.Abs(flagValue)
		os.MkdirAll(abs, 0o755)
		return abs, false
	}
	dir = filepath.Join(filepath.Dir(exe), "data")
	if writable(dir) {
		return dir, false
	}
	local := os.Getenv("LOCALAPPDATA")
	dir = filepath.Join(local, appDirName)
	// Data left by the former name stays where it is.
	if !exists(dir) && exists(filepath.Join(local, legacyAppDirName)) {
		dir = filepath.Join(local, legacyAppDirName)
	}
	os.MkdirAll(dir, 0o755)
	return dir, true
}

func writable(dir string) bool {
	if os.MkdirAll(dir, 0o755) != nil {
		return false
	}
	probe := filepath.Join(dir, ".write-test")
	if os.WriteFile(probe, []byte("x"), 0o644) != nil {
		return false
	}
	os.Remove(probe)
	return true
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// DBPath returns the database to open. A database under the former name is
// renamed (with its write-ahead log) to the new one. If that cannot be done,
// for example because the old version of the app still has it open, the old
// file is used as it is rather than starting an empty database beside it.
func DBPath(dir string) string {
	current := filepath.Join(dir, DBName)
	legacy := filepath.Join(dir, legacyDBName)
	if exists(current) || !exists(legacy) {
		return current
	}
	// The side files first, the main file last: if anything fails the files
	// already moved are put back.
	var moved [][2]string
	for _, suffix := range []string{"-shm", "-wal", ""} {
		from, to := legacy+suffix, current+suffix
		if !exists(from) {
			continue
		}
		if err := os.Rename(from, to); err != nil {
			log.Printf("không đổi được tên cơ sở dữ liệu cũ (%v); tiếp tục dùng tên cũ", err)
			for _, m := range moved {
				os.Rename(m[1], m[0])
			}
			return legacy
		}
		moved = append(moved, [2]string{from, to})
	}
	log.Printf("đã chuyển cơ sở dữ liệu từ tên cũ %s sang %s", legacyDBName, DBName)
	return current
}
