package report

type ImageReport struct {
	Version        int                  `json:"version"`
	Image          string               `json:"image"`
	Memory         MemoryReport         `json:"memory"`
	Hardware       HardwareReport       `json:"hardware"`
	Runtime        RuntimeReport        `json:"runtime"`
	Storage        StorageReport        `json:"storage"`
	AuthorityAudit AuthorityAuditReport `json:"authority_audit"`
}

func NewImageReport(image string) ImageReport {
	return ImageReport{
		Version: 1,
		Image:   image,
		Memory: MemoryReport{
			RootRegions:     []MemoryRootReport{},
			Arenas:          []ArenaReport{},
			ExecutorBudgets: []ExecutorBudgetReport{},
		},
		Hardware: HardwareReport{
			Claims:      []AuthorityRecord{},
			PCI:         []PCIReport{},
			Timers:      []TimerReport{},
			Locality:    []LocalityReport{},
			Framebuffer: FramebufferReport{},
		},
		Runtime: RuntimeReport{
			Executors:       []ExecutorReport{},
			Placement:       []PlacementReport{},
			Interrupts:      []AuthorityRecord{},
			Topics:          []TopicReport{},
			InterruptQueues: []InterruptQueueReport{},
			WakePaths:       []WakePathReport{},
		},
		Storage: StorageReport{
			NvmePaths: []NvmePathReport{},
			CoreLinks: []CoreLinkReport{},
		},
		AuthorityAudit: AuthorityAuditReport{
			MemoryRoots:    []AuthorityRecord{},
			Arenas:         []AuthorityRecord{},
			HardwareClaims: []AuthorityRecord{},
			Interrupts:     []AuthorityRecord{},
			Timers:         []AuthorityRecord{},
			Queues:         []AuthorityRecord{},
			Topics:         []AuthorityRecord{},
			WakeTargets:    []AuthorityRecord{},
			DMABuffers:     []AuthorityRecord{},
		},
	}
}

type MemoryReport struct {
	TotalBytes      uint64                 `json:"total_bytes"`
	RootRegions     []MemoryRootReport     `json:"root_regions"`
	Arenas          []ArenaReport          `json:"arenas"`
	ExecutorBudgets []ExecutorBudgetReport `json:"executor_budgets"`
}

type MemoryRootReport struct {
	Label string `json:"label"`
	Base  uint64 `json:"base"`
	Bytes uint64 `json:"bytes"`
}

type ArenaReport struct {
	Label  string `json:"label"`
	Parent string `json:"parent"`
	Base   uint64 `json:"base"`
	Bytes  uint64 `json:"bytes"`
	Owner  string `json:"owner"`
	Kind   string `json:"kind"`
}

type ExecutorBudgetReport struct {
	SlotLabel string `json:"slot_label"`
	Bytes     uint64 `json:"bytes"`
}

type HardwareReport struct {
	Claims      []AuthorityRecord `json:"claims"`
	PCI         []PCIReport       `json:"pci"`
	APIC        APICReport        `json:"apic"`
	Timers      []TimerReport     `json:"timers"`
	Locality    []LocalityReport  `json:"locality"`
	Framebuffer FramebufferReport `json:"framebuffer"`
}

type PCIReport struct {
	Identity string      `json:"identity"`
	BARs     []BARReport `json:"bars"`
}

type BARReport struct {
	Index uint8  `json:"index"`
	Kind  string `json:"kind"`
	Base  uint64 `json:"base"`
	Bytes uint64 `json:"bytes"`
}

type APICReport struct {
	Mode             string `json:"mode"`
	SelectedAPICMode uint32 `json:"selected_apic_mode"`
	Required         bool   `json:"required"`
	Fallback         string `json:"fallback,omitempty"`
}

type TimerReport struct {
	Label    string `json:"label"`
	Source   string `json:"source"`
	PeriodUS uint64 `json:"period_us"`
}

type LocalityReport struct {
	Subject string `json:"subject"`
	Kind    string `json:"kind"`
	Value   string `json:"value"`
	Known   bool   `json:"known"`
}

type FramebufferReport struct {
	Base   uint64 `json:"base"`
	Bytes  uint64 `json:"bytes"`
	Width  uint32 `json:"width"`
	Height uint32 `json:"height"`
	Stride uint32 `json:"stride"`
	Format uint32 `json:"format"`
	Known  bool   `json:"known"`
}

type RuntimeReport struct {
	Executors       []ExecutorReport       `json:"executors"`
	Placement       []PlacementReport      `json:"placement"`
	Interrupts      []AuthorityRecord      `json:"interrupts"`
	Topics          []TopicReport          `json:"topics"`
	InterruptQueues []InterruptQueueReport `json:"interrupt_queues"`
	WakePaths       []WakePathReport       `json:"wake_paths"`
}

type ExecutorReport struct {
	SlotLabel string `json:"slot_label"`
	VcpuID    uint64 `json:"vcpu_id"`
}

type PlacementReport struct {
	Kind      string `json:"kind"`
	SubjectA  string `json:"subject_a"`
	SubjectB  string `json:"subject_b"`
	Required  bool   `json:"required"`
	Satisfied bool   `json:"satisfied"`
	Fallback  string `json:"fallback"`
}

type TopicReport struct {
	Label       string `json:"label"`
	Type        string `json:"type,omitempty"`
	TypeKey     string `json:"type_key,omitempty"`
	PayloadType string `json:"payload_type"`
	PayloadKey  string `json:"payload_key,omitempty"`
	NextType    string `json:"next_type,omitempty"`
	NextKey     string `json:"next_key,omitempty"`
	Bytes       uint64 `json:"bytes"`
	Align       uint64 `json:"align"`
	Depth       uint64 `json:"depth"`
}

type InterruptQueueReport struct {
	Label    string `json:"label"`
	Owner    string `json:"owner"`
	Capacity uint64 `json:"capacity"`
	Overflow string `json:"overflow"`
}

type WakePathReport struct {
	SlotLabel string `json:"slot_label"`
	Strategy  string `json:"strategy"`
	Fallback  string `json:"fallback"`
}

type StorageReport struct {
	ActiveLBASize                    uint64           `json:"active_lba_size"`
	NamespaceMode                    string           `json:"namespace_mode"`
	DurabilityMode                   string           `json:"durability_mode"`
	InterruptMode                    string           `json:"interrupt_mode"`
	MsiFallbackSharesVector          bool             `json:"msi_fallback_shares_vector"`
	EventSlotSize                    uint64           `json:"event_slot_size"`
	EventHeaderSize                  uint64           `json:"event_header_size"`
	EventPayloadBytes                uint64           `json:"event_payload_bytes"`
	EventSlotsWritten                uint64           `json:"event_slots_written"`
	ReservedEmptySlots               uint64           `json:"reserved_empty_slots"`
	EventSlotsReservedEmpty          uint64           `json:"event_slots_reserved_empty"`
	EventSlotsRecovered              uint64           `json:"event_slots_recovered"`
	TargetBatchSlots                 uint64           `json:"target_batch_slots"`
	MaxOverflowSlots                 uint64           `json:"max_overflow_slots"`
	MaxBatchSlots                    uint64           `json:"max_batch_slots"`
	MaxAtomicGroupSlots              uint64           `json:"max_atomic_group_slots"`
	BatchesSubmitted                 uint64           `json:"batches_submitted"`
	BatchOverflowCount               uint64           `json:"batch_overflow_count"`
	AppendLatencyP50US               uint64           `json:"append_latency_p50_us"`
	AppendLatencyP99US               uint64           `json:"append_latency_p99_us"`
	DeviceReportedMediaWrites        uint64           `json:"device_reported_media_writes"`
	MediaWriteBytes                  uint64           `json:"media_write_bytes"`
	AdminQueueDepth                  uint64           `json:"admin_queue_depth"`
	ForegroundIOQueueDepth           uint64           `json:"foreground_io_queue_depth"`
	BackgroundIOQueueDepth           uint64           `json:"background_io_queue_depth"`
	BlobOrphanBytes                  uint64           `json:"blob_orphan_bytes"`
	ProjectionLagEvents              uint64           `json:"projection_lag_events"`
	EventUpcastCount                 uint64           `json:"event_upcast_count"`
	ProjectionUpcastCount            uint64           `json:"projection_upcast_count"`
	ProjectionRebuildCount           uint64           `json:"projection_rebuild_count"`
	StreamDirectoryCacheHits         uint64           `json:"stream_directory_cache_hits"`
	StreamDirectoryCacheMisses       uint64           `json:"stream_directory_cache_misses"`
	StreamDirectoryCacheHitRateX1000 uint64           `json:"stream_directory_cache_hit_rate_x1000"`
	CoreLinkCommittedGroups          uint64           `json:"core_link_committed_groups"`
	CoreLinkBackpressureCount        uint64           `json:"core_link_backpressure_count"`
	NvmePaths                        []NvmePathReport `json:"nvme_paths"`
	CoreLinks                        []CoreLinkReport `json:"core_links"`
}

type NvmePathReport struct {
	Label      string `json:"label"`
	Role       string `json:"role"`
	Owner      string `json:"owner"`
	QueueID    uint16 `json:"queue_id"`
	Vector     uint8  `json:"vector"`
	QueueDepth uint64 `json:"queue_depth"`
}

type CoreLinkReport struct {
	Label     string `json:"label"`
	Direction string `json:"direction"`
	Role      string `json:"role"`
	Owner     string `json:"owner"`
	Peer      string `json:"peer"`
	Depth     uint64 `json:"depth"`
}

type AuthorityAuditReport struct {
	MemoryRoots    []AuthorityRecord `json:"memory_roots"`
	Arenas         []AuthorityRecord `json:"arenas"`
	HardwareClaims []AuthorityRecord `json:"hardware_claims"`
	Interrupts     []AuthorityRecord `json:"interrupts"`
	Timers         []AuthorityRecord `json:"timers"`
	Queues         []AuthorityRecord `json:"queues"`
	Topics         []AuthorityRecord `json:"topics"`
	WakeTargets    []AuthorityRecord `json:"wake_targets"`
	DMABuffers     []AuthorityRecord `json:"dma_buffers"`
}

type AuthorityRecord struct {
	Kind       string `json:"kind"`
	Label      string `json:"label"`
	Owner      string `json:"owner"`
	Provenance string `json:"provenance"`
}
