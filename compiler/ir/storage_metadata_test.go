package ir

import "testing"

func TestStorageIRMetadataShape(t *testing.T) {
	program := Program{
		StorageEvents: []EventLayout{{
			Module:        "app",
			Name:          "FileCreated",
			EventTypeID:   1001,
			LayoutID:      1,
			Current:       true,
			PayloadSize:   40,
			PayloadAlign:  8,
			EncoderSymbol: "_wrela_storage_event_app_FileCreated_layout_1_encode",
		}},
		StorageProjections: []ProjectionLayout{{
			Module:       "app",
			Name:         "DirectoryChildren",
			ProjectionID: 12,
			LayoutID:     1,
			Current:      true,
		}},
	}
	if got := program.StorageEvents[0].EventTypeID; got != 1001 {
		t.Fatalf("event_type_id = %d, want 1001", got)
	}
	if got := program.StorageProjections[0].ProjectionID; got != 12 {
		t.Fatalf("projection id = %d, want 12", got)
	}
}

func TestCheckedStorageProgramForTestHasCurrentLayouts(t *testing.T) {
	checked := checkedStorageProgramForTest(t)
	event := checked.Storage.EventsByTypeID[1001]
	if event.CurrentLayoutID != 1 || len(event.Layouts) != 1 || !event.Layouts[0].Current {
		t.Fatalf("event storage metadata = %#v", event)
	}
	projection := checked.Storage.ProjectionsByID[12]
	if projection.CurrentLayoutID != 1 || len(projection.Layouts) != 1 || !projection.Layouts[0].Current {
		t.Fatalf("projection storage metadata = %#v", projection)
	}
}
