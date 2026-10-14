package config

import (
	"testing"
)

func TestProductionRefusesInsecureDatabase(t *testing.T) {
	c := &Config{Env: "production", Origin: "https://quiz.example", LogLevel: "info", DBMaxConns: 10, AIMaxInFlight: 4, CSRFSecret: "0123456789abcdef0123456789abcdef", MailProvider: "mock"}
	c.DatabaseURL = "postgresql://u:p@localhost:5432/db?sslmode=disable"
	if err := c.Validate(); err == nil {
		t.Fatal("localhost DB accepted in production")
	}
	c.DatabaseURL = "postgresql://u:p@host-pooler.aws.neon.tech/db?sslmode=disable"
	if err := c.Validate(); err == nil {
		t.Fatal("non-TLS DB accepted in production")
	}
	c.DatabaseURL = "postgresql://u:p@host-pooler.aws.neon.tech/db?sslmode=require"
	if err := c.Validate(); err != nil {
		t.Fatalf("TLS Neon URL rejected: %v", err)
	}
	// Pooled runtime vs direct migration URLs are both accepted structurally.
	c.MigrationsDatabaseURL = "postgresql://u:p@host.aws.neon.tech/db?sslmode=require"
	if err := c.Validate(); err != nil {
		t.Fatalf("direct migration URL rejected: %v", err)
	}
}
