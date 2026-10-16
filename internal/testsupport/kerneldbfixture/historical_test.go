package kerneldbfixture

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/m0n0x41d/haft/db"
)

func TestCreateSchema57KeepsHistoricalSchemaUntilExplicitMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "historical.db")
	if err := CreateSchema57(path); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := CreateSchema57(path); err == nil {
		t.Fatal("fixture overwrote an existing database")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("rejected fixture creation changed existing bytes")
	}
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var version int
	if err := conn.QueryRow("SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 57 {
		t.Fatalf("historical schema = %d, want 57", version)
	}
	if err := db.RunMigrations(conn); err != nil {
		t.Fatal(err)
	}
	current, err := db.CurrentSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow("SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != current {
		t.Fatalf("upgraded schema = %d, want %d", version, current)
	}
}
