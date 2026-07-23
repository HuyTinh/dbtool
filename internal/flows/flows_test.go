package flows

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "flows.json"))
}

func TestRecordDeduplicatesAndRefreshesRecent(t *testing.T) {
	store := testStore(t)
	first := Flow{Operation: OperationDump, Profile: "source", FilePath: "/backups/a.dump", Settings: Settings{Format: "custom"}}
	if err := store.Record(first); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(Flow{Operation: OperationDump, Profile: "source", FilePath: "/backups/a.dump", Settings: Settings{Format: "directory"}}); err != nil {
		t.Fatal(err)
	}

	data, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Recent) != 1 {
		t.Fatalf("recent count = %d, want 1", len(data.Recent))
	}
	if got := data.Recent[0].Settings.Format; got != "directory" {
		t.Fatalf("format = %q, want updated format", got)
	}
	if data.Recent[0].LastUsed.IsZero() {
		t.Fatal("LastUsed was not populated")
	}
}

func TestRecordEnforcesRecentLimitAndPinEnforcesPinnedLimit(t *testing.T) {
	store := testStore(t)
	for i := 0; i < MaxRecent+2; i++ {
		flow := Flow{Operation: OperationDump, Profile: "source", FilePath: filepath.Join("/backups", string(rune('a'+i))) + ".dump"}
		if err := store.Record(flow); err != nil {
			t.Fatal(err)
		}
	}
	data, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Recent) != MaxRecent {
		t.Fatalf("recent count = %d, want %d", len(data.Recent), MaxRecent)
	}
	for i := 0; i < MaxPinned+1; i++ {
		if err := store.SetPinned(data.Recent[i], true); err != nil {
			t.Fatal(err)
		}
	}
	data, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Pinned) != MaxPinned {
		t.Fatalf("pinned count = %d, want %d", len(data.Pinned), MaxPinned)
	}
}

func TestFlowFileIsAtomicPrivateAndContainsNoSecrets(t *testing.T) {
	store := testStore(t)
	flow := Flow{
		Operation:          OperationMigrate,
		Profile:            "source",
		DestinationProfile: "target",
		Settings:           Settings{Format: "custom", Jobs: 4, Clean: true, IncludeTables: []string{"public.users"}},
	}
	if err := store.Record(flow); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	contents := strings.ToLower(string(data))
	for _, forbidden := range []string{"password", "secret", "token", "host", "database", "user"} {
		if strings.Contains(contents, "\""+forbidden+"\"") {
			t.Fatalf("flows file contains forbidden field %q: %s", forbidden, data)
		}
	}
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if got := info.Mode().Perm(); got != 0600 {
			t.Fatalf("permissions = %o, want 0600", got)
		}
	}
	if matches, err := filepath.Glob(store.Path() + ".tmp-*"); err != nil || len(matches) != 0 {
		t.Fatalf("temporary files left behind: %v, %v", matches, err)
	}
}

func TestPinnedFlowsAreUpdatedWhenRecordedAgain(t *testing.T) {
	store := testStore(t)
	flow := Flow{Operation: OperationRestore, Profile: "target", FilePath: "/backups/app.dump", Settings: Settings{Jobs: 2}}
	if err := store.Record(flow); err != nil {
		t.Fatal(err)
	}
	if err := store.SetPinned(flow, true); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(Flow{Operation: OperationRestore, Profile: "target", FilePath: "/backups/app.dump", Settings: Settings{Jobs: 8}}); err != nil {
		t.Fatal(err)
	}
	data, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := data.Pinned[0].Settings.Jobs; got != 8 {
		t.Fatalf("pinned jobs = %d, want 8", got)
	}
	if data.Pinned[0].LastUsed.Before(time.Now().Add(-time.Minute)) {
		t.Fatal("pinned last-used was not refreshed")
	}
}
