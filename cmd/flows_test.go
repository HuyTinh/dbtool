package cmd

import (
	"testing"

	"dbtool/internal/flows"
)

func TestFlowBuildersContainOnlyReusableOperationSettings(t *testing.T) {
	dump := dumpFlow("source", "/safe/backup.dump", "custom", []string{"public.users"}, nil, nil, nil)
	if dump.Operation != flows.OperationDump || dump.Profile != "source" || dump.FilePath != "/safe/backup.dump" {
		t.Fatalf("unexpected dump flow: %#v", dump)
	}
	if len(dump.Settings.IncludeTables) != 1 || dump.DestinationProfile != "" {
		t.Fatalf("unexpected dump settings: %#v", dump)
	}

	restore := restoreFlow("target", "/safe/backup.dump", "directory", 4, true, true, true, nil, []string{"audit"}, nil, nil)
	if restore.Operation != flows.OperationRestore || !restore.Settings.Clean || !restore.Settings.CreateIfMissing || !restore.Settings.Optimize {
		t.Fatalf("unexpected restore flow: %#v", restore)
	}

	migrate := migrateFlow("source", "target", "custom", 2, true, false, true, true, nil, nil, []string{"public"}, nil)
	if migrate.Operation != flows.OperationMigrate || migrate.DestinationProfile != "target" || !migrate.Settings.SchemaOnly || !migrate.Settings.Clean {
		t.Fatalf("unexpected migrate flow: %#v", migrate)
	}
}
