package shortcode

import "testing"

func TestEncode(t *testing.T) {
	tests := []struct {
		name string
		in   uint64
		want string
	}{
		{"zero", 0, "0"},
		{"one", 1, "1"},
		{"ten", 10, "a"},
		{"sixty-one", 61, "Z"},
		{"sixty-two", 62, "10"},
		{"larger value", 123456789, "8m0Kx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Encode(tt.in); got != tt.want {
				t.Errorf("Encode(%d) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
