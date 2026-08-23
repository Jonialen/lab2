package main

import (
	"strings"
	"testing"
)

func TestEnvIntOrDefault(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		want      int
		wantError string
	}{
		{name: "missing uses fallback", want: 9000},
		{name: "valid value", value: "9100", want: 9100},
		{name: "invalid value fails", value: "not-a-port", wantError: `invalid PORT "not-a-port": must be an integer`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("PORT", test.value)
			got, err := envIntOrDefault("PORT", 9000)
			if test.wantError == "" {
				if err != nil || got != test.want {
					t.Fatalf("envIntOrDefault() = %d, %v; want %d, nil", got, err, test.want)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("envIntOrDefault() error = %v; want %q", err, test.wantError)
			}
		})
	}
}
