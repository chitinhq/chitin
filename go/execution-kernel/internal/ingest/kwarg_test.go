package ingest

import "testing"

func TestGetKwargInt(t *testing.T) {
	t.Run("missing key", func(t *testing.T) {
		m := map[string]interface{}{"other": 42}
		v, ok := getKwargInt(m, "count")
		if ok {
			t.Error("expected ok=false for missing key")
		}
		if v != 0 {
			t.Errorf("expected 0, got %d", v)
		}
	})

	t.Run("float64", func(t *testing.T) {
		m := map[string]interface{}{"count": float64(7)}
		v, ok := getKwargInt(m, "count")
		if !ok {
			t.Fatal("expected ok=true")
		}
		if v != 7 {
			t.Errorf("expected 7, got %d", v)
		}
	})

	t.Run("float32", func(t *testing.T) {
		m := map[string]interface{}{"count": float32(3)}
		v, ok := getKwargInt(m, "count")
		if !ok {
			t.Fatal("expected ok=true")
		}
		if v != 3 {
			t.Errorf("expected 3, got %d", v)
		}
	})

	t.Run("int", func(t *testing.T) {
		m := map[string]interface{}{"count": int(42)}
		v, ok := getKwargInt(m, "count")
		if !ok {
			t.Fatal("expected ok=true")
		}
		if v != 42 {
			t.Errorf("expected 42, got %d", v)
		}
	})

	t.Run("int64", func(t *testing.T) {
		m := map[string]interface{}{"count": int64(99)}
		v, ok := getKwargInt(m, "count")
		if !ok {
			t.Fatal("expected ok=true")
		}
		if v != 99 {
			t.Errorf("expected 99, got %d", v)
		}
	})

	t.Run("wrong type string", func(t *testing.T) {
		m := map[string]interface{}{"count": "seven"}
		v, ok := getKwargInt(m, "count")
		if ok {
			t.Error("expected ok=false for string value")
		}
		if v != 0 {
			t.Errorf("expected 0, got %d", v)
		}
	})

	t.Run("wrong type bool", func(t *testing.T) {
		m := map[string]interface{}{"count": true}
		_, ok := getKwargInt(m, "count")
		if ok {
			t.Error("expected ok=false for bool value")
		}
	})
}

func TestGetKwargFloat(t *testing.T) {
	t.Run("missing key", func(t *testing.T) {
		m := map[string]interface{}{"other": 1.0}
		v, ok := getKwargFloat(m, "ratio")
		if ok {
			t.Error("expected ok=false for missing key")
		}
		if v != 0 {
			t.Errorf("expected 0, got %f", v)
		}
	})

	t.Run("float64", func(t *testing.T) {
		m := map[string]interface{}{"ratio": float64(3.14)}
		v, ok := getKwargFloat(m, "ratio")
		if !ok {
			t.Fatal("expected ok=true")
		}
		if v != 3.14 {
			t.Errorf("expected 3.14, got %f", v)
		}
	})

	t.Run("float32", func(t *testing.T) {
		m := map[string]interface{}{"ratio": float32(2.5)}
		v, ok := getKwargFloat(m, "ratio")
		if !ok {
			t.Fatal("expected ok=true")
		}
		// float32(2.5) → float64 may not be exactly 2.5
		if v < 2.49 || v > 2.51 {
			t.Errorf("expected ~2.5, got %f", v)
		}
	})

	t.Run("int", func(t *testing.T) {
		m := map[string]interface{}{"ratio": int(5)}
		v, ok := getKwargFloat(m, "ratio")
		if !ok {
			t.Fatal("expected ok=true")
		}
		if v != 5.0 {
			t.Errorf("expected 5.0, got %f", v)
		}
	})

	t.Run("int64", func(t *testing.T) {
		m := map[string]interface{}{"ratio": int64(10)}
		v, ok := getKwargFloat(m, "ratio")
		if !ok {
			t.Fatal("expected ok=true")
		}
		if v != 10.0 {
			t.Errorf("expected 10.0, got %f", v)
		}
	})

	t.Run("wrong type string", func(t *testing.T) {
		m := map[string]interface{}{"ratio": "fast"}
		_, ok := getKwargFloat(m, "ratio")
		if ok {
			t.Error("expected ok=false for string value")
		}
	})
}