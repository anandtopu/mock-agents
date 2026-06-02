package config

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// billionLaughs builds the classic YAML alias-expansion bomb: each level is a
// 9-element sequence of references to the level below, so N levels expand to
// 9^N nodes. With the bomb wired into a decoded field (metadata), a decoder
// that resolves aliases without a budget would expand ~9^9 ≈ 387M nodes and
// hang / OOM.
func billionLaughs() []byte {
	var b strings.Builder
	b.WriteString("kind: Agent\n")
	b.WriteString(`a: &a ["lol","lol","lol","lol","lol","lol","lol","lol","lol"]` + "\n")
	prev := "a"
	for _, name := range []string{"b", "c", "d", "e", "f", "g", "h", "i"} {
		b.WriteString(name + ": &" + name + " [")
		for j := range 9 {
			if j > 0 {
				b.WriteString(",")
			}
			b.WriteString("*" + prev)
		}
		b.WriteString("]\n")
		prev = name
	}
	// Force expansion through a decoded field.
	b.WriteString("metadata: *i\n")
	return []byte(b.String())
}

// runWithTimeout runs fn and returns false if it doesn't finish within d —
// used to detect an unbounded alias expansion (a hang) rather than blocking
// the whole test binary.
func runWithTimeout(d time.Duration, fn func()) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// TestYAMLBillionLaughs_Bounded confirms that the YAML library (yaml.v3) caps
// alias expansion, so a billion-laughs document is rejected quickly instead of
// hanging/OOMing the process (X-DOS-001 YAML half). It characterizes both the
// raw decoder and the ValidateBytes path the GUI /editor exercises.
func TestYAMLBillionLaughs_Bounded(t *testing.T) {
	bomb := billionLaughs()

	t.Run("raw decode into interface forces full expansion", func(t *testing.T) {
		var out any
		var err error
		finished := runWithTimeout(5*time.Second, func() {
			err = yaml.Unmarshal(bomb, &out)
		})
		if !finished {
			t.Fatal("yaml.Unmarshal did not return within 5s — alias expansion is unbounded (DoS)")
		}
		if err == nil {
			t.Error("expected an error from the billion-laughs bomb, got nil")
		} else {
			t.Logf("decoder rejected the bomb: %v", err)
		}
	})

	t.Run("ValidateBytes is bounded", func(t *testing.T) {
		var report *ValidateReport
		finished := runWithTimeout(5*time.Second, func() {
			report = ValidateBytes(bomb)
		})
		if !finished {
			t.Fatal("ValidateBytes did not return within 5s — the body cap alone is insufficient")
		}
		if report == nil || len(report.Errors) == 0 {
			t.Errorf("expected ValidateBytes to surface a parse error for the bomb, got %+v", report)
		}
	})
}
