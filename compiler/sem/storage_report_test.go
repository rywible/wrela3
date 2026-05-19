package sem

import (
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/storagefmt"
)

func TestStorageMetricsReportPopulation(t *testing.T) {
	checked := &CheckedProgram{
		ImageGraph: ImageGraph{
			StoragePaths: []StoragePathNode{
				{Label: "nvme.foreground", Role: "foreground", Owner: "foreground", QueueID: 1, Vector: 80},
				{Label: "nvme.background", Role: "background", Owner: "maintenance", QueueID: 2, Vector: 81},
			},
			CoreLinkEndpoints: []CoreLinkEndpointNode{
				{Label: "core_link.producer.0", Direction: "tx", Role: "producer", Owner: "foreground", Peer: "maintenance", Depth: 64},
				{Label: "core_link.consumer.1", Direction: "rx", Role: "consumer", Owner: "maintenance", Peer: "foreground", Depth: 64},
			},
			ProjectionFeeds: []ProjectionFeedNode{
				{Projection: "DirectoryChildren", SourceLabel: "core_link.consumer.1", Owner: "maintenance"},
			},
			StorageWriters: []StorageWriterNode{
				{Phase: "owned_hardware", PathRoles: map[string]string{"foreground": "foreground", "background": "background"}},
			},
			StorageAppendCalls: []StorageAppendCallNode{
				{ResultObserved: true},
			},
		},
		Storage: StorageIndex{
			ProjectionsByID: map[uint64]ProjectionInfo{
				12: {
					Name:            "DirectoryChildren",
					ProjectionID:    12,
					CurrentLayoutID: 2,
					Layouts: []ProjectionLayoutInfo{
						{ID: 1},
						{ID: 2},
					},
				},
			},
		},
	}

	r := BuildImageReport(checked)
	if r.Storage.ActiveLBASize != 512 ||
		r.Storage.NamespaceMode != "conventional" ||
		r.Storage.DurabilityMode != "fua" ||
		r.Storage.EventSlotSize != 512 {
		t.Fatalf("storage format metrics = %#v", r.Storage)
	}
	if r.Storage.TargetBatchSlots != 64 ||
		r.Storage.MaxOverflowSlots != 8 ||
		r.Storage.MaxBatchSlots != 72 ||
		r.Storage.MaxAtomicGroupSlots != 32 {
		t.Fatalf("storage batch metrics = %#v", r.Storage)
	}
	if r.Storage.ReservedEmptySlots != storagefmt.FinishBatch(r.Storage.ActiveLBASize, r.Storage.TargetBatchSlots).ReservedEmptySlots {
		t.Fatalf("reserved empty slots = %d", r.Storage.ReservedEmptySlots)
	}
	if r.Storage.AdminQueueDepth != 32 ||
		r.Storage.ForegroundIOQueueDepth != 256 ||
		r.Storage.BackgroundIOQueueDepth != 128 {
		t.Fatalf("storage queue depths = %#v", r.Storage)
	}
	if r.Storage.DeviceReportedMediaWrites == 0 || r.Storage.MediaWriteBytes == 0 {
		t.Fatalf("storage media-write metrics = %#v", r.Storage)
	}
	if r.Storage.AppendLatencyP50US == 0 || r.Storage.AppendLatencyP99US == 0 {
		t.Fatalf("storage latency metrics = %#v", r.Storage)
	}
	if r.Storage.BlobOrphanBytes != storagefmt.EventPayloadBytes {
		t.Fatalf("blob orphan bytes = %d, want %d", r.Storage.BlobOrphanBytes, storagefmt.EventPayloadBytes)
	}
	if r.Storage.ProjectionLagEvents != 1 ||
		r.Storage.ProjectionUpcastCount != 1 ||
		r.Storage.ProjectionRebuildCount != 1 {
		t.Fatalf("storage projection metrics = %#v", r.Storage)
	}
	if r.Storage.StreamDirectoryCacheHitRateX1000 != 1000 {
		t.Fatalf("stream directory cache hit rate = %d, want 1000", r.Storage.StreamDirectoryCacheHitRateX1000)
	}
	if len(r.Storage.NvmePaths) != 2 || r.Storage.NvmePaths[0].QueueDepth != 256 || r.Storage.NvmePaths[1].QueueDepth != 128 {
		t.Fatalf("storage paths = %#v", r.Storage.NvmePaths)
	}
	if len(r.Storage.CoreLinks) != 2 || r.Storage.CoreLinks[0].Depth != 64 {
		t.Fatalf("core links = %#v", r.Storage.CoreLinks)
	}
	if ds := ValidateStorageReportContent(r); len(ds) != 0 {
		t.Fatalf("storage report diagnostics = %#v, want none", ds)
	}
}

func TestStorageReportMissingMetricsEmitsSEM0124(t *testing.T) {
	r := BuildImageReport(&CheckedProgram{
		ImageGraph: ImageGraph{
			StoragePaths: []StoragePathNode{
				{Label: "nvme.foreground", Role: "foreground"},
				{Label: "nvme.background", Role: "background"},
			},
			CoreLinkEndpoints:  []CoreLinkEndpointNode{{Label: "core_link.producer.0"}},
			ProjectionFeeds:    []ProjectionFeedNode{{Projection: "DirectoryChildren"}},
			StorageAppendCalls: []StorageAppendCallNode{{ResultObserved: true}},
		},
		Storage: StorageIndex{
			ProjectionsByID: map[uint64]ProjectionInfo{
				12: {
					Name:         "DirectoryChildren",
					ProjectionID: 12,
					Layouts: []ProjectionLayoutInfo{
						{ID: 1},
						{ID: 2},
					},
				},
			},
		},
	})
	r.Storage.EventSlotSize = 0
	r.Storage.BlobOrphanBytes = 0
	r.Storage.ProjectionLagEvents = 0
	r.Storage.ProjectionUpcastCount = 0
	r.Storage.ProjectionRebuildCount = 0

	ds := ValidateStorageReportContent(r)
	if !hasCode(ds, diag.SEM0124) {
		t.Fatalf("diagnostics = %#v, want SEM0124", ds)
	}
	for _, metric := range []string{
		"event_slot_size",
		"blob_orphan_bytes",
		"projection_lag_events",
		"projection_upcast_count",
		"projection_rebuild_count",
	} {
		if !hasDiagnosticMessage(ds, metric) {
			t.Fatalf("diagnostics = %#v, want missing %s", ds, metric)
		}
	}
}

func TestStorageReportMissingForegroundOrBackgroundPathEmitsSEM0124(t *testing.T) {
	r := BuildImageReport(&CheckedProgram{
		ImageGraph: ImageGraph{
			StoragePaths: []StoragePathNode{{Label: "nvme.foreground", Role: "foreground"}},
		},
	})

	ds := ValidateStorageReportContent(r)
	if !hasDiagnosticMessage(ds, "nvme_paths.background") {
		t.Fatalf("diagnostics = %#v, want missing background nvme path", ds)
	}
}

func hasDiagnosticMessage(ds []diag.Diagnostic, want string) bool {
	for _, d := range ds {
		if strings.Contains(d.Message, want) {
			return true
		}
	}
	return false
}
