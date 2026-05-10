package ingest

import (
	"testing"
)

func TestGetKwargInt(t *testing.T) {
	m := map[string]interface{}{}

	// Missing key
	if v, ok := getKwargInt(m, "x"); ok || v != 0 {
		t.Errorf("missing key: got (%d, %v), want (0, false)", v, ok)
	}

	// float64
	m["a"] = float64(42)
	if v, ok := getKwargInt(m, "a"); !ok || v != 42 {
		t.Errorf("float64: got (%d, %v), want (42, true)", v, ok)
	}

	// float32
	m["b"] = float32(10)
	if v, ok := getKwargInt(m, "b"); !ok || v != 10 {
		t.Errorf("float32: got (%d, %v), want (10, true)", v, ok)
	}

	// int
	m["c"] = int(7)
	if v, ok := getKwargInt(m, "c"); !ok || v != 7 {
		t.Errorf("int: got (%d, %v), want (7, true)", v, ok)
	}

	// int64
	m["d"] = int64(99)
	if v, ok := getKwargInt(m, "d"); !ok || v != 99 {
		t.Errorf("int64: got (%d, %v), want (99, true)", v, ok)
	}

	// Wrong type
	m["e"] = "not a number"
	if v, ok := getKwargInt(m, "e"); ok || v != 0 {
		t.Errorf("string: got (%d, %v), want (0, false)", v, ok)
	}
}

func TestGetKwargFloat(t *testing.T) {
	m := map[string]interface{}{}

	// Missing key
	if v, ok := getKwargFloat(m, "x"); ok || v != 0 {
		t.Errorf("missing key: got (%f, %v), want (0, false)", v, ok)
	}

	// float64
	m["a"] = float64(3.14)
	if v, ok := getKwargFloat(m, "a"); !ok || v != 3.14 {
		t.Errorf("float64: got (%f, %v), want (3.14, true)", v, ok)
	}

	// float32
	m["b"] = float32(2.5)
	if v, ok := getKwargFloat(m, "b"); !ok {
		t.Errorf("float32: got (%f, %v), want true", v, ok)
	}

	// int
	m["c"] = int(42)
	if v, ok := getKwargFloat(m, "c"); !ok || v != 42.0 {
		t.Errorf("int: got (%f, %v), want (42.0, true)", v, ok)
	}

	// int64
	m["d"] = int64(100)
	if v, ok := getKwargFloat(m, "d"); !ok || v != 100.0 {
		t.Errorf("int64: got (%f, %v), want (100.0, true)", v, ok)
	}

	// Wrong type
	m["e"] = "not a number"
	if v, ok := getKwargFloat(m, "e"); ok || v != 0 {
		t.Errorf("string: got (%f, %v), want (0, false)", v, ok)
	}
}