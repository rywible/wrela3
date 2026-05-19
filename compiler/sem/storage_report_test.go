package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
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
			StoragePaths: []StoragePathNode{{Label: "nvme.foreground", Role: "foreground"}},
		},
	})
	r.Storage.EventSlotSize = 0

	ds := ValidateStorageReportContent(r)
	if !hasCode(ds, diag.SEM0124) {
		t.Fatalf("diagnostics = %#v, want SEM0124", ds)
	}
}
