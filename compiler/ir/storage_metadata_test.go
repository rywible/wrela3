package ir

import "testing"

func TestStorageIRMetadataShape(t *testing.T) {
	program := Program{
		StorageEvents: []EventLayout{{
			Module:       "app",
			Name:         "FileCreated",
			EventTypeID:  1001,
			LayoutID:     1,
			Current:      true,
			PayloadSize:  40,
			PayloadAlign: 8,
			PayloadFields: []EventPayloadField{{
				Name:        "file_id",
				Type:        Type{Name: "U64", Module: "builtin", Kind: TypeKindPrimitive},
				Offset:      0,
				StorageSize: 8,
				Align:       8,
			}},
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

func TestLowerStorageEventMetadata(t *testing.T) {
	checked := checkedStorageProgramForTest(t)
	program, ds := Lower(checked)
	if len(ds) != 0 {
		t.Fatalf("lower diagnostics: %#v", ds)
	}
	if len(program.StorageEvents) != 1 {
		t.Fatalf("storage events = %d, want 1", len(program.StorageEvents))
	}
	event := program.StorageEvents[0]
	if event.Module != "app" || event.Name != "FileCreated" || event.EventTypeID != 1001 || event.LayoutID != 1 || !event.Current {
		t.Fatalf("event metadata = %#v", event)
	}
	if event.EncoderSymbol != "_wrela_storage_event_app_FileCreated_layout_1_encode" {
		t.Fatalf("encoder symbol = %q", event.EncoderSymbol)
	}
	if event.PayloadSize == 0 || event.PayloadAlign == 0 {
		t.Fatalf("payload layout not populated: %#v", event)
	}
	if len(event.PayloadFields) != 1 {
		t.Fatalf("payload fields = %#v, want one field", event.PayloadFields)
	}
	field := event.PayloadFields[0]
	if field.Name != "file_id" || field.Offset != 0 || field.Type.Name != "U64" || field.StorageSize != 8 || field.Align != 8 {
		t.Fatalf("payload field metadata = %#v", field)
	}
	if len(program.StorageProjections) != 1 {
		t.Fatalf("storage projections = %d, want 1", len(program.StorageProjections))
	}
	projection := program.StorageProjections[0]
	if projection.Module != "app" || projection.Name != "DirectoryChildren" || projection.ProjectionID != 12 || projection.LayoutID != 1 || !projection.Current {
		t.Fatalf("projection metadata = %#v", projection)
	}
	if len(projection.ContainerKinds) != 1 || projection.ContainerKinds[0] != "OrderedPages" {
		t.Fatalf("projection container kinds = %#v", projection.ContainerKinds)
	}
}
