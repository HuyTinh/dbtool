package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	"github.com/spf13/cobra"
)

var schemaDiffFrom, schemaDiffTo, schemaDiffOutput string

type columnAttributeOptions struct {
	profile      string
	schema       string
	table        string
	column       string
	nullable     bool
	nullableSet  bool
	defaultValue string
	defaultSet   bool
	dropDefault  bool
	unique       bool
	uniqueSet    bool
	constraint   string
	apply        bool
	output       string
}

var schemaColumnAttributes columnAttributeOptions

var schemaCmd = &cobra.Command{Use: "schema", Short: "Inspect and safely alter PostgreSQL schema metadata"}
var schemaDiffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Compare read-only schema catalogs between two profiles",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithTimeout(cmd, cmd.Context(), executeSchemaDiff)
	},
}

var schemaColumnAlterCmd = &cobra.Command{
	Use:   "alter",
	Short: "Preview or apply PostgreSQL NULL, DEFAULT, and UNIQUE column attributes",
	RunE: func(cmd *cobra.Command, args []string) error {
		schemaColumnAttributes.nullableSet = cmd.Flags().Changed("nullable")
		schemaColumnAttributes.defaultSet = cmd.Flags().Changed("default")
		schemaColumnAttributes.uniqueSet = cmd.Flags().Changed("unique")
		return runWithTimeout(cmd, cmd.Context(), executeSchemaColumnAlter)
	},
}

func executeSchemaDiff(ctx context.Context) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	src, ok := cfg.GetProfile(schemaDiffFrom)
	if !ok {
		return fmt.Errorf("source profile %q not found", schemaDiffFrom)
	}
	dst, ok := cfg.GetProfile(schemaDiffTo)
	if !ok {
		return fmt.Errorf("target profile %q not found", schemaDiffTo)
	}
	if src.Name == dst.Name {
		return fmt.Errorf("source and target profiles must be different")
	}
	if src.Driver != dst.Driver {
		return fmt.Errorf("cross-driver schema diff is not supported (source: %s, target: %s)", src.Driver, dst.Driver)
	}
	drv, err := driver.Get(src.Driver)
	if err != nil {
		return err
	}
	inspector, ok := drv.(driver.SchemaInspector)
	if !ok {
		return fmt.Errorf("driver %q does not support schema snapshots", src.Driver)
	}
	source, err := inspector.CollectSchema(ctx, src)
	if err != nil {
		return err
	}
	target, err := inspector.CollectSchema(ctx, dst)
	if err != nil {
		return err
	}
	diff := driver.CompareSchemaSnapshots(source, target)
	if schemaDiffOutput == "json" {
		return json.NewEncoder(os.Stdout).Encode(diff)
	}
	if schemaDiffOutput != "table" {
		return fmt.Errorf("unsupported output %q (want table or json)", schemaDiffOutput)
	}
	fmt.Printf("Schema Diff: %s → %s\n", src.Name, dst.Name)
	for _, table := range diff.MissingTables {
		fmt.Printf("MISSING TABLE: %s\n", table)
	}
	for _, table := range diff.ExtraTables {
		fmt.Printf("EXTRA TABLE: %s\n", table)
	}
	for _, column := range diff.ColumnDifferences {
		fmt.Printf("COLUMN DIFFERENCE: %s.%s\n", column.Table, column.Column)
	}
	if len(diff.MissingTables)+len(diff.ExtraTables)+len(diff.ColumnDifferences) == 0 {
		fmt.Println("No catalog differences found.")
	}
	return nil
}

func executeSchemaColumnAlter(ctx context.Context) error {
	change, err := columnAttributeChangeFromOptions(schemaColumnAttributes)
	if err != nil {
		return err
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	profile, ok := cfg.GetProfile(schemaColumnAttributes.profile)
	if !ok {
		return fmt.Errorf("profile %q not found", schemaColumnAttributes.profile)
	}
	drv, err := driver.Get(profile.Driver)
	if err != nil {
		return err
	}
	editor, ok := drv.(driver.ColumnAttributeEditor)
	if !ok {
		return fmt.Errorf("driver %q does not support column attribute changes", profile.Driver)
	}

	var plan *driver.ColumnAttributePlan
	if schemaColumnAttributes.apply {
		plan, err = editor.ApplyColumnAttributes(ctx, profile, change)
	} else {
		plan, err = editor.PreflightColumnAttributes(ctx, profile, change)
	}
	if err != nil {
		return err
	}
	return renderColumnAttributePlan(plan, schemaColumnAttributes.apply, schemaColumnAttributes.output)
}

func columnAttributeChangeFromOptions(opts columnAttributeOptions) (driver.ColumnAttributeChange, error) {
	change := driver.ColumnAttributeChange{
		Schema:      opts.schema,
		Table:       opts.table,
		Column:      opts.column,
		DropDefault: opts.dropDefault,
		Constraint:  opts.constraint,
	}
	if opts.nullableSet {
		value := opts.nullable
		change.Nullable = &value
	}
	if opts.defaultSet {
		value := opts.defaultValue
		change.Default = &value
	}
	if opts.uniqueSet {
		value := opts.unique
		change.Unique = &value
	}
	return change, nil
}

func renderColumnAttributePlan(plan *driver.ColumnAttributePlan, applied bool, output string) error {
	if output == "json" {
		return json.NewEncoder(os.Stdout).Encode(plan)
	}
	if output != "table" {
		return fmt.Errorf("unsupported output %q (want table or json)", output)
	}
	if applied {
		fmt.Println("Column attribute change applied after locked preflight:")
	} else {
		fmt.Println("Column attribute preflight (read-only):")
	}
	fmt.Printf("  Target: %s.%s.%s\n", plan.Change.Schema, plan.Change.Table, plan.Change.Column)
	if plan.NullRows > 0 {
		fmt.Printf("  NULL rows: %d\n", plan.NullRows)
	}
	if plan.DuplicateGroups > 0 {
		fmt.Printf("  Duplicate non-NULL groups: %d (%d rows)\n", plan.DuplicateGroups, plan.DuplicateRows)
	}
	if len(plan.ExistingUniqueConstraints) > 0 {
		fmt.Printf("  Existing UNIQUE constraints: %v\n", plan.ExistingUniqueConstraints)
	}
	for _, warning := range plan.Warnings {
		fmt.Printf("  WARNING: %s\n", warning)
	}
	fmt.Println("SQL:")
	for _, statement := range plan.Statements {
		fmt.Printf("  %s;\n", statement)
	}
	if !applied {
		fmt.Println("Nothing was changed. Re-run with --apply to execute after a fresh locked preflight.")
	}
	return nil
}

func init() {
	schemaDiffCmd.Flags().StringVar(&schemaDiffFrom, "source", "", "Source PostgreSQL profile")
	schemaDiffCmd.Flags().StringVar(&schemaDiffTo, "target", "", "Target PostgreSQL profile")
	schemaDiffCmd.Flags().StringVar(&schemaDiffOutput, "output", "table", "Output format: table or json")
	_ = schemaDiffCmd.MarkFlagRequired("source")
	_ = schemaDiffCmd.MarkFlagRequired("target")
	schemaColumnAlterCmd.Flags().StringVar(&schemaColumnAttributes.profile, "profile", "", "PostgreSQL profile name")
	schemaColumnAlterCmd.Flags().StringVar(&schemaColumnAttributes.schema, "schema", "public", "PostgreSQL schema name")
	schemaColumnAlterCmd.Flags().StringVar(&schemaColumnAttributes.table, "table", "", "Table name")
	schemaColumnAlterCmd.Flags().StringVar(&schemaColumnAttributes.column, "column", "", "Column name")
	schemaColumnAlterCmd.Flags().BoolVar(&schemaColumnAttributes.nullable, "nullable", false, "Set nullable=true or nullable=false")
	schemaColumnAlterCmd.Flags().StringVar(&schemaColumnAttributes.defaultValue, "default", "", "PostgreSQL SQL expression to set as DEFAULT")
	schemaColumnAlterCmd.Flags().BoolVar(&schemaColumnAttributes.dropDefault, "drop-default", false, "Remove the current DEFAULT")
	schemaColumnAlterCmd.Flags().BoolVar(&schemaColumnAttributes.unique, "unique", false, "Set unique=true or unique=false")
	schemaColumnAlterCmd.Flags().StringVar(&schemaColumnAttributes.constraint, "constraint", "", "UNIQUE constraint name (optional when unambiguous)")
	schemaColumnAlterCmd.Flags().BoolVar(&schemaColumnAttributes.apply, "apply", false, "Execute after a fresh locked preflight (default is preview only)")
	schemaColumnAlterCmd.Flags().StringVar(&schemaColumnAttributes.output, "output", "table", "Output format: table or json")
	_ = schemaColumnAlterCmd.MarkFlagRequired("profile")
	_ = schemaColumnAlterCmd.MarkFlagRequired("table")
	_ = schemaColumnAlterCmd.MarkFlagRequired("column")
	schemaCmd.AddCommand(schemaDiffCmd)
	schemaCmd.AddCommand(schemaColumnAlterCmd)
	RootCmd.AddCommand(schemaCmd)
}
