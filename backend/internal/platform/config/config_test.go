package config

import (
	"testing"
)

func TestValidateEnv(t *testing.T) {
	c := &Config{Env: "development", Origin: "http://localhost:5173", LogLevel: "info", DBMaxConns: 10, AIMaxInFlight: 4}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	c.Env = "bogus"
	if err := c.Validate(); err == nil {
		t.Fatal("expected invalid env to fail")
	}
}
