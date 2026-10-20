package verifier

import "testing"

func TestDurationRejectsUnknownAndNonFinite(t *testing.T) {
	for _, value := range []string{"", "N/A", "NaN", "Inf", "-Inf", "0", "-3", "12junk"} {
		if _, err := parseDuration(value); err == nil {
			t.Errorf("accepted invalid duration %q", value)
		}
	}
	if got, err := parseDuration("123.456"); err != nil || got != 123.456 {
		t.Fatalf("valid duration: %v, %v", got, err)
	}
}
