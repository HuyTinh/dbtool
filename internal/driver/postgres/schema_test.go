package postgres

import (
	"context"
	"net/url"
	"reflect"
	"testing"

	"dbtool/internal/config"
)

func TestEnsureSchemasCreatesEachSchemaOnce(t *testing.T) {
	var statements []string

	err := ensureSchemas(context.Background(), []string{"tenant", "public", "tenant", ""}, func(_ context.Context, statement string) error {
		statements = append(statements, statement)
		return nil
	})
	if err != nil {
		t.Fatalf("ensureSchemas: %v", err)
	}

	want := []string{
		`CREATE SCHEMA IF NOT EXISTS "tenant"`,
		`CREATE SCHEMA IF NOT EXISTS "public"`,
	}
	if !reflect.DeepEqual(statements, want) {
		t.Fatalf("statements = %#v, want %#v", statements, want)
	}
}

func TestBuildPostgresDSNEncodesCredentials(t *testing.T) {
	profile := config.Profile{
		Host:     "db.example.test",
		Port:     5432,
		User:     "user@team",
		Password: "p@ss:/?word",
		Database: "inventory db",
	}

	parsed, err := url.Parse(buildPostgresDSN(profile, 3))
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	password, ok := parsed.User.Password()
	if !ok {
		t.Fatal("DSN is missing password")
	}
	if parsed.Scheme != "postgres" || parsed.User.Username() != profile.User || password != profile.Password {
		t.Fatalf("credentials = %q/%q, want %q/%q", parsed.User.Username(), password, profile.User, profile.Password)
	}
	if parsed.Host != "db.example.test:5432" || parsed.Path != "/inventory db" {
		t.Fatalf("target = %q%s, want db.example.test:5432/inventory db", parsed.Host, parsed.Path)
	}
	if parsed.Query().Get("connect_timeout") != "3" {
		t.Fatalf("connect_timeout = %q, want 3", parsed.Query().Get("connect_timeout"))
	}
}
