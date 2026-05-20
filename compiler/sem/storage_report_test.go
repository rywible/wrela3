package sem

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/source"
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

func TestStorageReportDurabilityModeUsesSelectedMetricsLiteral(t *testing.T) {
	checked := storageReportCheckedProgramWithMetricsExpr(&ast.IntLiteral{Value: "3"})

	r := BuildImageReport(checked)
	if r.Storage.DurabilityMode != "write_plus_flush" {
		t.Fatalf("durability mode = %q, want write_plus_flush", r.Storage.DurabilityMode)
	}
}

func TestStorageReportDurabilityModeUsesRuntimeNvmeFacts(t *testing.T) {
	checked := storageReportCheckedProgramWithMetricsExpr(&ast.CallExpr{Method: "first_append_durability_mode_value"})
	checked.Modules = append(checked.Modules, &ast.Module{
		Name: "machine.x86_64.nvme",
		Decls: []ast.Decl{&ast.ClassDecl{
			Name: "NvmeDirectStorage",
			Methods: []ast.MethodDecl{{
				Name: "identify_controller",
				Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.ConstructorExpr{
					Type: ast.TypeRef{Name: "NvmeControllerFacts"},
					Args: []ast.NamedArg{{Name: "supports_fua", Value: &ast.BoolLiteral{Value: false}}},
				}}},
			}},
		}},
	})

	r := BuildImageReport(checked)
	if r.Storage.DurabilityMode != "write_plus_flush" {
		t.Fatalf("durability mode = %q, want write_plus_flush", r.Storage.DurabilityMode)
	}
}

func TestStorageReportUsesStorageMetricsConstructorFacts(t *testing.T) {
	checked := storageReportCheckedProgramWithMetricsExpr(&ast.IntLiteral{Value: "1"})
	checked.Modules[0].Decls = []ast.Decl{&ast.ImageDecl{
		Name: "StorageReportImage",
		Phases: []ast.PhaseDecl{{
			Body: []ast.Stmt{&ast.LetStmt{
				Name: "metrics",
				Expr: &ast.ConstructorExpr{
					Type: ast.TypeRef{Name: "StorageMetrics"},
					Args: []ast.NamedArg{
						{Name: "selected_durability_mode", Value: &ast.IntLiteral{Value: "1"}},
						{Name: "foreground_path_queue_id", Value: &ast.IntLiteral{Value: "3"}},
						{Name: "foreground_path_vector", Value: &ast.IntLiteral{Value: "90"}},
						{Name: "background_path_queue_id", Value: &ast.IntLiteral{Value: "4"}},
						{Name: "background_path_vector", Value: &ast.IntLiteral{Value: "91"}},
						{Name: "event_slot_size", Value: &ast.IntLiteral{Value: "4096"}},
						{Name: "event_header_size", Value: &ast.IntLiteral{Value: "64"}},
						{Name: "event_payload_bytes", Value: &ast.IntLiteral{Value: "4032"}},
						{Name: "event_slots_written", Value: &ast.IntLiteral{Value: "5"}},
						{Name: "event_slots_reserved_empty", Value: &ast.IntLiteral{Value: "3"}},
						{Name: "event_slots_recovered", Value: &ast.IntLiteral{Value: "4"}},
						{Name: "target_batch_slots", Value: &ast.IntLiteral{Value: "7"}},
						{Name: "max_batch_slots", Value: &ast.IntLiteral{Value: "9"}},
						{Name: "batches_submitted", Value: &ast.IntLiteral{Value: "6"}},
						{Name: "batch_overflow_count", Value: &ast.IntLiteral{Value: "1"}},
						{Name: "append_latency_us", Value: &ast.IntLiteral{Value: "123"}},
						{Name: "durability_latency_us", Value: &ast.IntLiteral{Value: "4567"}},
						{Name: "device_media_write_commands", Value: &ast.IntLiteral{Value: "22"}},
						{Name: "device_media_write_bytes", Value: &ast.IntLiteral{Value: "90112"}},
						{Name: "blob_orphan_bytes", Value: &ast.IntLiteral{Value: "333"}},
						{Name: "admin_queue_depth", Value: &ast.IntLiteral{Value: "11"}},
						{Name: "foreground_io_queue_depth", Value: &ast.IntLiteral{Value: "77"}},
						{Name: "background_io_queue_depth", Value: &ast.IntLiteral{Value: "55"}},
						{Name: "projection_lag_events", Value: &ast.IntLiteral{Value: "44"}},
						{Name: "stream_directory_cache_hits", Value: &ast.IntLiteral{Value: "10"}},
						{Name: "stream_directory_cache_misses", Value: &ast.IntLiteral{Value: "6"}},
						{Name: "core_link_committed_groups", Value: &ast.IntLiteral{Value: "8"}},
						{Name: "core_link_backpressure_count", Value: &ast.IntLiteral{Value: "1"}},
						{Name: "event_upcast_count", Value: &ast.IntLiteral{Value: "4"}},
						{Name: "projection_upcast_count", Value: &ast.IntLiteral{Value: "3"}},
						{Name: "projection_rebuild_count", Value: &ast.IntLiteral{Value: "2"}},
						{Name: "stream_directory_cache_hit_rate_ppm", Value: &ast.IntLiteral{Value: "625000"}},
					},
				},
			}},
		}},
	}}
	checked.ImageGraph.CoreLinkEndpoints = []CoreLinkEndpointNode{{Label: "core_link.producer.0", Depth: 16}}
	checked.ImageGraph.ProjectionFeeds = []ProjectionFeedNode{{Projection: "DirectoryChildren"}}
	checked.Storage.ProjectionsByID = map[uint64]ProjectionInfo{
		12: {Name: "DirectoryChildren", ProjectionID: 12, Layouts: []ProjectionLayoutInfo{{ID: 1}, {ID: 2}}},
	}

	r := BuildImageReport(checked)
	if r.Storage.EventSlotSize != 4096 ||
		r.Storage.EventHeaderSize != 64 ||
		r.Storage.EventPayloadBytes != 4032 ||
		r.Storage.EventSlotsWritten != 5 ||
		r.Storage.EventSlotsReservedEmpty != 3 ||
		r.Storage.EventSlotsRecovered != 4 ||
		r.Storage.TargetBatchSlots != 7 ||
		r.Storage.MaxBatchSlots != 9 ||
		r.Storage.BatchesSubmitted != 6 ||
		r.Storage.BatchOverflowCount != 1 ||
		r.Storage.AppendLatencyP50US != 123 ||
		r.Storage.AppendLatencyP99US != 4567 ||
		r.Storage.DeviceReportedMediaWrites != 22 ||
		r.Storage.MediaWriteBytes != 90112 ||
		r.Storage.BlobOrphanBytes != 333 ||
		r.Storage.AdminQueueDepth != 11 ||
		r.Storage.ForegroundIOQueueDepth != 77 ||
		r.Storage.BackgroundIOQueueDepth != 55 ||
		r.Storage.ProjectionLagEvents != 44 ||
		r.Storage.StreamDirectoryCacheHits != 10 ||
		r.Storage.StreamDirectoryCacheMisses != 6 ||
		r.Storage.CoreLinkCommittedGroups != 8 ||
		r.Storage.CoreLinkBackpressureCount != 1 ||
		r.Storage.EventUpcastCount != 4 ||
		r.Storage.ProjectionUpcastCount != 3 ||
		r.Storage.ProjectionRebuildCount != 2 ||
		r.Storage.StreamDirectoryCacheHitRateX1000 != 625 {
		t.Fatalf("storage metrics were not constructor-backed: %#v", r.Storage)
	}
	if len(r.Storage.NvmePaths) != 2 || r.Storage.NvmePaths[0].QueueDepth != 77 || r.Storage.NvmePaths[1].QueueDepth != 55 {
		t.Fatalf("storage path queue depths = %#v", r.Storage.NvmePaths)
	}
	if r.Storage.NvmePaths[0].QueueID != 3 || r.Storage.NvmePaths[0].Vector != 90 ||
		r.Storage.NvmePaths[1].QueueID != 4 || r.Storage.NvmePaths[1].Vector != 91 {
		t.Fatalf("storage path queue/vector facts = %#v", r.Storage.NvmePaths)
	}
	if r.Storage.InterruptMode != "planned_vectors" || r.Storage.MsiFallbackSharesVector {
		t.Fatalf("storage interrupt mode = %q shared=%v", r.Storage.InterruptMode, r.Storage.MsiFallbackSharesVector)
	}
}

func TestStorageReportUsesStorageMetricsConstantFacts(t *testing.T) {
	checked := storageReportCheckedProgramWithMetricsExpr(&ast.IntLiteral{Value: "1"})
	checked.Index = NewIndex()
	checked.Index.Consts["storage.report.test"] = map[string]ConstValue{
		"FOREGROUND_DEPTH": {Type: &Type{}, Value: 99},
	}
	checked.Modules[0].Decls = []ast.Decl{&ast.ImageDecl{
		Name: "StorageReportImage",
		Phases: []ast.PhaseDecl{{
			Body: []ast.Stmt{&ast.LetStmt{
				Name: "metrics",
				Expr: &ast.ConstructorExpr{
					Type: ast.TypeRef{Name: "StorageMetrics"},
					Args: []ast.NamedArg{
						{Name: "selected_durability_mode", Value: &ast.IntLiteral{Value: "1"}},
						{Name: "foreground_io_queue_depth", Value: &ast.NameExpr{Name: "FOREGROUND_DEPTH"}},
					},
				},
			}},
		}},
	}}

	r := BuildImageReport(checked)
	if r.Storage.ForegroundIOQueueDepth != 99 || len(r.Storage.NvmePaths) != 2 || r.Storage.NvmePaths[0].QueueDepth != 99 {
		t.Fatalf("constant-backed foreground depth not reported: storage=%#v paths=%#v", r.Storage, r.Storage.NvmePaths)
	}
}

func TestStorageReportUsesNvmeFixtureStorageMetricsFacts(t *testing.T) {
	checked := checkedStorageReportProgramAt(t, "tests/e2e/fixtures/nvme_event_storage/main.wrela")
	facts := storageMetricsFacts(checked)
	if _, ok := facts["blob_orphan_bytes"]; !ok {
		t.Fatalf("storage metrics facts missing fixture constructor values: %#v", facts)
	}
	if _, ok := facts["event_slots_reserved_empty"]; !ok {
		t.Fatalf("storage metrics facts missing fixture helper values: %#v", facts)
	}

	r := BuildImageReport(checked)
	if r.Storage.InterruptMode != "msix_or_multimessage_msi" || r.Storage.MsiFallbackSharesVector {
		t.Fatalf("fixture interrupt mode = %q shared=%v", r.Storage.InterruptMode, r.Storage.MsiFallbackSharesVector)
	}
	for name, got := range map[string]uint64{
		"event_header_size":                   r.Storage.EventHeaderSize,
		"event_payload_bytes":                 r.Storage.EventPayloadBytes,
		"event_slots_written":                 r.Storage.EventSlotsWritten,
		"event_slots_reserved_empty":          r.Storage.EventSlotsReservedEmpty,
		"event_slots_recovered":               r.Storage.EventSlotsRecovered,
		"blob_orphan_bytes":                   r.Storage.BlobOrphanBytes,
		"projection_lag_events":               r.Storage.ProjectionLagEvents,
		"stream_directory_cache_hits":         r.Storage.StreamDirectoryCacheHits,
		"stream_directory_cache_misses":       r.Storage.StreamDirectoryCacheMisses,
		"stream_directory_cache_hit_rate_ppm": r.Storage.StreamDirectoryCacheHitRateX1000 * 1000,
		"batches_submitted":                   r.Storage.BatchesSubmitted,
		"batch_overflow_count":                r.Storage.BatchOverflowCount,
		"core_link_committed_groups":          r.Storage.CoreLinkCommittedGroups,
		"core_link_backpressure_count":        r.Storage.CoreLinkBackpressureCount,
		"event_upcast_count":                  r.Storage.EventUpcastCount,
		"projection_upcast_count":             r.Storage.ProjectionUpcastCount,
		"projection_rebuild_count":            r.Storage.ProjectionRebuildCount,
	} {
		if got != facts[name] {
			t.Fatalf("fixture storage report %s = %d, want constructor fact %d", name, got, facts[name])
		}
	}
	if r.Storage.DeviceReportedMediaWrites == 0 ||
		r.Storage.MediaWriteBytes == 0 ||
		r.Storage.AppendLatencyP50US == 0 ||
		r.Storage.AppendLatencyP99US == 0 ||
		r.Storage.StreamDirectoryCacheHitRateX1000 == 0 {
		t.Fatalf("fixture storage report required metrics are zero: %#v", r.Storage)
	}
}

func checkedStorageReportProgramAt(t *testing.T, rootPath string) *CheckedProgram {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	graph, err := source.LoadGraph(source.Options{
		RootPath: filepath.Join(repoRoot, rootPath),
		ImportRoots: []string{
			repoRoot,
			filepath.Join(repoRoot, "wrela"),
		},
	})
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	modules, ds := parse.ParseGraph(*graph)
	if len(ds) != 0 {
		t.Fatalf("ParseGraph diagnostics: %#v", ds)
	}
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("BuildIndex diagnostics: %#v", ds)
	}
	checked, ds := Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("Check diagnostics: %#v", ds)
	}
	return checked
}

func storageReportCheckedProgramWithMetricsExpr(expr ast.Expr) *CheckedProgram {
	return &CheckedProgram{
		Modules: []*ast.Module{{
			Name: "storage.report.test",
			Decls: []ast.Decl{&ast.ImageDecl{
				Name: "StorageReportImage",
				Phases: []ast.PhaseDecl{{
					Body: []ast.Stmt{&ast.LetStmt{
						Name: "metrics",
						Expr: &ast.ConstructorExpr{
							Type: ast.TypeRef{Name: "StorageMetrics"},
							Args: []ast.NamedArg{{Name: "selected_durability_mode", Value: expr}},
						},
					}},
				}},
			}},
		}},
		ImageGraph: ImageGraph{
			StoragePaths: []StoragePathNode{
				{Label: "nvme.foreground", Role: "foreground", Owner: "foreground", QueueID: 1, Vector: 80},
				{Label: "nvme.background", Role: "background", Owner: "maintenance", QueueID: 2, Vector: 81},
			},
			StorageAppendCalls: []StorageAppendCallNode{{ResultObserved: true}},
		},
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
	r.Storage.EventHeaderSize = 0
	r.Storage.EventPayloadBytes = 0

	ds := ValidateStorageReportContent(r)
	if !hasCode(ds, diag.SEM0124) {
		t.Fatalf("diagnostics = %#v, want SEM0124", ds)
	}
	for _, metric := range []string{
		"event_slot_size",
		"event_header_size",
		"event_payload_bytes",
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
