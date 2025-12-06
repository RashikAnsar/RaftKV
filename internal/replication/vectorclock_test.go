package replication

import (
	"testing"
)

func TestVectorClock_IncrementAndGet(t *testing.T) {
	vc := NewVectorClock()

	// Initial value should be 0
	if vc.Get("dc1") != 0 {
		t.Error("Expected initial value to be 0")
	}

	// Increment and check
	vc.Increment("dc1")
	if vc.Get("dc1") != 1 {
		t.Errorf("Expected 1, got %d", vc.Get("dc1"))
	}

	vc.Increment("dc1")
	if vc.Get("dc1") != 2 {
		t.Errorf("Expected 2, got %d", vc.Get("dc1"))
	}
}

func TestVectorClock_Update(t *testing.T) {
	vc := NewVectorClock()

	// Update to higher value
	vc.Update("dc1", 5)
	if vc.Get("dc1") != 5 {
		t.Errorf("Expected 5, got %d", vc.Get("dc1"))
	}

	// Update to lower value (should not change)
	vc.Update("dc1", 3)
	if vc.Get("dc1") != 5 {
		t.Errorf("Expected 5 (should not decrease), got %d", vc.Get("dc1"))
	}

	// Update to higher value again
	vc.Update("dc1", 10)
	if vc.Get("dc1") != 10 {
		t.Errorf("Expected 10, got %d", vc.Get("dc1"))
	}
}

func TestVectorClock_Merge(t *testing.T) {
	vc1 := NewVectorClock()
	vc1.Update("dc1", 5)
	vc1.Update("dc2", 3)

	vc2 := NewVectorClock()
	vc2.Update("dc1", 3)
	vc2.Update("dc2", 7)
	vc2.Update("dc3", 2)

	// Merge vc2 into vc1
	vc1.Merge(vc2)

	// Should have max values
	if vc1.Get("dc1") != 5 {
		t.Errorf("dc1: expected 5, got %d", vc1.Get("dc1"))
	}
	if vc1.Get("dc2") != 7 {
		t.Errorf("dc2: expected 7, got %d", vc1.Get("dc2"))
	}
	if vc1.Get("dc3") != 2 {
		t.Errorf("dc3: expected 2, got %d", vc1.Get("dc3"))
	}
}

func TestVectorClock_Compare_Equal(t *testing.T) {
	vc1 := NewVectorClock()
	vc1.Update("dc1", 5)
	vc1.Update("dc2", 3)

	vc2 := NewVectorClock()
	vc2.Update("dc1", 5)
	vc2.Update("dc2", 3)

	if vc1.Compare(vc2) != ClockEqual {
		t.Error("Clocks should be equal")
	}

	if !vc1.IsEqual(vc2) {
		t.Error("IsEqual should return true")
	}
}

func TestVectorClock_Compare_Before(t *testing.T) {
	vc1 := NewVectorClock()
	vc1.Update("dc1", 3)
	vc1.Update("dc2", 2)

	vc2 := NewVectorClock()
	vc2.Update("dc1", 5)
	vc2.Update("dc2", 4)

	if vc1.Compare(vc2) != ClockBefore {
		t.Errorf("vc1 should happen before vc2, got %s", vc1.Compare(vc2))
	}

	if !vc1.HappenedBefore(vc2) {
		t.Error("HappenedBefore should return true")
	}
}

func TestVectorClock_Compare_After(t *testing.T) {
	vc1 := NewVectorClock()
	vc1.Update("dc1", 5)
	vc1.Update("dc2", 4)

	vc2 := NewVectorClock()
	vc2.Update("dc1", 3)
	vc2.Update("dc2", 2)

	if vc1.Compare(vc2) != ClockAfter {
		t.Errorf("vc1 should happen after vc2, got %s", vc1.Compare(vc2))
	}

	if !vc1.HappenedAfter(vc2) {
		t.Error("HappenedAfter should return true")
	}
}

func TestVectorClock_Compare_Concurrent(t *testing.T) {
	vc1 := NewVectorClock()
	vc1.Update("dc1", 5)
	vc1.Update("dc2", 2)

	vc2 := NewVectorClock()
	vc2.Update("dc1", 3)
	vc2.Update("dc2", 4)

	if vc1.Compare(vc2) != ClockConcurrent {
		t.Errorf("Clocks should be concurrent, got %s", vc1.Compare(vc2))
	}

	if !vc1.IsConcurrent(vc2) {
		t.Error("IsConcurrent should return true")
	}
}

func TestVectorClock_Clone(t *testing.T) {
	vc1 := NewVectorClock()
	vc1.Update("dc1", 5)
	vc1.Update("dc2", 3)

	vc2 := vc1.Clone()

	// Should be equal
	if !vc1.IsEqual(vc2) {
		t.Error("Clone should be equal to original")
	}

	// Modifying clone should not affect original
	vc2.Update("dc1", 10)
	if vc1.Get("dc1") != 5 {
		t.Error("Modifying clone should not affect original")
	}
	if vc2.Get("dc1") != 10 {
		t.Error("Clone was not modified correctly")
	}
}

func TestVectorClock_ToMap(t *testing.T) {
	vc := NewVectorClock()
	vc.Update("dc1", 5)
	vc.Update("dc2", 3)

	m := vc.ToMap()

	if m["dc1"] != 5 {
		t.Errorf("dc1: expected 5, got %d", m["dc1"])
	}
	if m["dc2"] != 3 {
		t.Errorf("dc2: expected 3, got %d", m["dc2"])
	}

	// Modifying map should not affect vector clock
	m["dc1"] = 10
	if vc.Get("dc1") != 5 {
		t.Error("Modifying map should not affect vector clock")
	}
}

func TestVectorClock_NewVectorClockFrom(t *testing.T) {
	m := map[string]uint64{
		"dc1": 5,
		"dc2": 3,
		"dc3": 7,
	}

	vc := NewVectorClockFrom(m)

	if vc.Get("dc1") != 5 {
		t.Error("Failed to initialize from map")
	}
	if vc.Get("dc2") != 3 {
		t.Error("Failed to initialize from map")
	}
	if vc.Get("dc3") != 7 {
		t.Error("Failed to initialize from map")
	}
}
