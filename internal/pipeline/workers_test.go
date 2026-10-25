package pipeline

import "testing"

func TestLeaseLimit(t *testing.T) {
	ocr := Capability{TaskType: "ocr", Concurrency: 2}
	archive := Capability{TaskType: "archive", Concurrency: 1}
	caps := []Capability{ocr, archive}
	one := []LeasedTask{{Type: "ocr"}}
	for _, c := range []struct {
		name     string
		capacity int
		leased   []LeasedTask
		cap      Capability
		active   int
		want     int
	}{
		{"first type gets the capacity", 1, nil, ocr, 0, 1},
		{"capacity counts every task type", 1, one, archive, 1, 0},
		{"capacity 0 means one", 0, nil, ocr, 0, 1},
		{"capped by the type's concurrency", 10, nil, ocr, 0, 2},
		{"room left for the next type", 10, one, archive, 1, 1},
		{"worker already busy", 10, nil, ocr, 3, 0},
	} {
		if got := leaseLimit(c.capacity, c.leased, c.cap, caps, c.active); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}
