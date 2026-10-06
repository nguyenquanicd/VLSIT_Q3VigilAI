package memtrim

import (
	"runtime"
	"testing"
)

func TestTrimEmptiesTheWorkingSet(t *testing.T) {
	if WorkingSet() == 0 {
		t.Fatal("WorkingSet could not be read")
	}
	// Touch 96 MB so that it is resident, then let go of it.
	big := make([]byte, 96<<20)
	for i := 0; i < len(big); i += 4096 {
		big[i] = 1
	}
	held := WorkingSet()
	if held < 90<<20 {
		t.Fatalf("test setup: only %d MB resident after touching 96 MB", held>>20)
	}
	big = nil
	runtime.GC()
	before, after := Trim()
	if before < 90<<20 {
		t.Errorf("before = %d MB", before>>20)
	}
	if after > before/2 {
		t.Errorf("working set %d MB -> %d MB: not trimmed", before>>20, after>>20)
	}
	t.Logf("working set %d MB -> %d MB", before>>20, after>>20)
	// The process keeps working: pages come back on demand.
	again := make([]byte, 8<<20)
	for i := range again {
		again[i] = 2
	}
	if again[len(again)-1] != 2 {
		t.Error("memory unusable after a trim")
	}
}
