package driver

import "testing"

func TestSessionSnapshotAndEntryExposeReadOnlyFields(t *testing.T) {
	snapshot := SessionSnapshot{Sessions: []SessionEntry{{PID: 42, Database: "app", User: "reader", State: "active", WaitEventType: "Lock", WaitEvent: "transactionid", QueryAgeMS: 1200, BlockingPIDs: []int32{7}}}}
	if len(snapshot.Sessions) != 1 || snapshot.Sessions[0].PID != 42 || snapshot.Sessions[0].User != "reader" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}
