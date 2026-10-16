package db

import (
	"bytes"
	"database/sql"
	"strings"
	"testing"
)

func TestExecutionRolePolicyMigration60PreservesHistoricalPermission(t *testing.T) {
	database := newStrictPermissionDatabase47(t)
	defer database.Close()
	if err := RunMigrationsThrough(database, 59); err != nil {
		t.Fatal(err)
	}
	var before []byte
	if err := database.QueryRow("SELECT canonical_bytes FROM project_typeenv_head_selection_permissions_v3").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	assertMigrationVersionPresent(t, database, 60)
	var after []byte
	if err := database.QueryRow("SELECT canonical_bytes FROM project_typeenv_head_selection_permissions_v3").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("migration rewrote historical Permission bytes")
	}
	assertTypedMemoryForeignKeysClean45(t, database)
	if _, err := database.Exec("UPDATE project_typeenv_head_selection_permissions_v3 SET canonical_bytes = 'tampered'"); err == nil {
		t.Fatal("migration lost immutable Permission trigger")
	}
	if err := RunMigrations(database); err != nil {
		t.Fatal(err)
	}
}

// Exercise the migrated CHECK independently of the source-closure triggers;
// the preservation test above keeps those triggers and foreign keys enforced.
func TestExecutionRolePolicyMigration60EnforcesEditionWindows(t *testing.T) {
	database := newStrictPermissionDatabase47(t)
	defer database.Close()
	if err := RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	var ddl string
	if err := database.QueryRow("SELECT sql FROM sqlite_master WHERE name = 'project_typeenv_head_selection_permissions_v3'").Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	rows, err := database.Query("SELECT * FROM project_typeenv_head_selection_permissions_v3")
	if err != nil {
		t.Fatal(err)
	}
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	values := make([]any, len(columns))
	pointers := make([]any, len(columns))
	for i := range values {
		pointers[i] = &values[i]
	}
	if !rows.Next() {
		t.Fatal("historical Permission missing")
	}
	if err := rows.Scan(pointers...); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, edition, from, until string
		accepted                   bool
	}{
		{"historical v1", "v1", "2026-07-17T00:00:00Z", "2026-07-17T01:00:00Z", true},
		{"v1 boundary", "v1", "2026-09-18T23:00:00Z", "2026-09-18T23:59:59Z", true},
		{"expired v1", "v1", "2026-09-19T00:00:00Z", "2026-09-19T01:00:00Z", false},
		{"new v2 after v1 expiry", "v2", "2026-09-19T00:00:00Z", "2026-09-19T01:00:00Z", true},
		{"v2 boundary", "v2", "2027-12-31T23:00:00Z", "2027-12-31T23:59:59Z", true},
		{"expired v2", "v2", "2028-01-01T00:00:00Z", "2028-01-01T01:00:00Z", false},
		{"unknown edition", "v3", "2026-09-19T00:00:00Z", "2026-09-19T01:00:00Z", false},
		{"before baseline", "v2", "2026-07-14T00:00:00Z", "2026-07-14T01:00:00Z", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolated, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer isolated.Close()
			if _, err := isolated.Exec(ddl); err != nil {
				t.Fatal(err)
			}
			row := append([]any(nil), values...)
			for i, column := range columns {
				switch column {
				case "subject_assignment_policy_edition_ref":
					row[i] = "policy-edition:project-typeenv-head-selection-execution-role/" + test.edition
				case "subject_assignment_from", "effective_from":
					row[i] = test.from
				case "subject_assignment_until", "validity_until":
					row[i] = test.until
				}
			}
			_, err = isolated.Exec("INSERT INTO project_typeenv_head_selection_permissions_v3 VALUES ("+strings.TrimSuffix(strings.Repeat("?,", len(row)), ",")+")", row...)
			if (err == nil) != test.accepted {
				t.Fatalf("accepted=%t, want %t: %v", err == nil, test.accepted, err)
			}
			if err != nil && !strings.Contains(err.Error(), "CHECK constraint failed") {
				t.Fatalf("unexpected rejection: %v", err)
			}
		})
	}
}

func TestExecutionRolePolicyMigration60RollsBackAndRestoresForeignKeys(t *testing.T) {
	database := newStrictPermissionDatabase47(t)
	defer database.Close()
	if err := RunMigrationsThrough(database, 59); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TRIGGER reject_role_policy_migration BEFORE INSERT ON schema_version
 WHEN NEW.version = 60 BEGIN SELECT RAISE(ABORT, 'injected version failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(database); err == nil || !strings.Contains(err.Error(), "injected version failure") {
		t.Fatalf("expected atomic migration failure, got %v", err)
	}
	var ddl string
	if err := database.QueryRow("SELECT sql FROM sqlite_master WHERE name = 'project_typeenv_head_selection_permissions_v3'").Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	if ddl != projectTypeEnvPermissionsV3Table47() {
		t.Fatal("failed migration changed published Permission table")
	}
	var foreignKeys int
	if err := database.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatal("migration left foreign keys disabled")
	}
	assertTypedMemoryTableRowCount45(t, database, "project_typeenv_head_selection_permissions_v3", 1)
	if _, err := database.Exec("DELETE FROM project_typeenv_head_selection_permissions_v3"); err == nil {
		t.Fatal("rollback lost immutability trigger")
	}
	if _, err := database.Exec("DROP TRIGGER reject_role_policy_migration"); err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	assertMigrationVersionPresent(t, database, 60)
	assertTypedMemoryForeignKeysClean45(t, database)
}
