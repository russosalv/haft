package db

import (
	"fmt"
	"strings"
)

// Published migration 47 remains immutable. Rebuild only its Permission table,
// preserving historical rows and all indexes/triggers (including dependents).
// Old binaries require the recovery snapshot once schema 60 has been installed.
var executionRolePolicyMigration60 = Migration{
	Version:            60,
	Description:        "Admit execution RolePolicy v2 without extending historical v1 assignments",
	ServeActivation:    ServeActivationAutomaticWithSnapshot,
	Apply:              applyExecutionRolePolicyMigration60,
	ApplyBoundary:      ForeignKeyTableRebuildBoundary,
	ForeignKeyVerifier: verifyForeignKeys,
}

func applyExecutionRolePolicyMigration60(tx MigrationTransaction, _ []Migration) error {
	const table = "project_typeenv_head_selection_permissions_v3"
	var source string
	if err := tx.QueryRow("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&source); err != nil {
		return fmt.Errorf("inspect execution RolePolicy Permission table: %w", err)
	}
	// Fail closed on unknown DDL rather than dropping unrecognized constraints.
	if source != projectTypeEnvPermissionsV3Table47() {
		return fmt.Errorf("execution RolePolicy migration requires exact published v47 Permission table")
	}
	rows, err := tx.Query(`SELECT type, name, sql FROM sqlite_master
 WHERE sql IS NOT NULL AND (
  (type = 'index' AND tbl_name = ?) OR
  (type = 'trigger' AND (tbl_name = ? OR instr(sql, ?) > 0))
 ) ORDER BY type, name`, table, table, table)
	if err != nil {
		return err
	}
	type dependent struct{ kind, name, statement string }
	var dependents []dependent
	for rows.Next() {
		var object dependent
		if err := rows.Scan(&object.kind, &object.name, &object.statement); err != nil {
			rows.Close()
			return err
		}
		dependents = append(dependents, object)
	}
	err = rows.Err()
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	for _, object := range dependents {
		if _, err := tx.Exec("DROP " + strings.ToUpper(object.kind) + ` "` + strings.ReplaceAll(object.name, `"`, `""`) + `"`); err != nil {
			return err
		}
	}
	replacement := strings.Replace(projectTypeEnvPermissionsV3Table60(), "CREATE TABLE "+table, "CREATE TABLE "+table+"_v60", 1)
	for _, statement := range []string{
		replacement,
		"INSERT INTO " + table + "_v60 SELECT * FROM " + table,
		"DROP TABLE " + table,
		"ALTER TABLE " + table + "_v60 RENAME TO " + table,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("rebuild execution RolePolicy Permission table: %w", err)
		}
	}
	for _, object := range dependents {
		if _, err := tx.Exec(object.statement); err != nil {
			return fmt.Errorf("restore %s: %w", object.name, err)
		}
	}
	return nil
}

func projectTypeEnvPermissionsV3Table60() string {
	const v1 = "'policy-edition:project-typeenv-head-selection-execution-role/v1'"
	const v2 = "'policy-edition:project-typeenv-head-selection-execution-role/v2'"
	ddl := strings.Replace(projectTypeEnvPermissionsV3Table47(),
		"subject_assignment_policy_edition_ref =\n\t\t\t\t"+v1,
		"subject_assignment_policy_edition_ref IN ("+v1+", "+v2+")", 1)
	return strings.Replace(ddl, "julianday('2026-09-18T23:59:59Z')",
		"julianday(CASE subject_assignment_policy_edition_ref WHEN "+v1+
			" THEN '2026-09-18T23:59:59Z' WHEN "+v2+" THEN '2027-12-31T23:59:59Z' END)", 1)
}
