package report

type ImageReport struct {
	Version        int                  `json:"version"`
	Image          string               `json:"image"`
	Memory         MemoryReport         `json:"memory"`
	Hardware       HardwareReport       `json:"hardware"`
	Runtime        RuntimeReport        `json:"runtime"`
	AuthorityAudit AuthorityAuditReport `json:"authority_audit"`
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
}

type ExecutorBudgetReport struct {
	SlotLabel string `json:"slot_label"`
	Bytes     uint64 `json:"bytes"`
}

type HardwareReport struct {
	PCI      []PCIReport      `json:"pci"`
	APIC     APICReport       `json:"apic"`
	Timers   []TimerReport    `json:"timers"`
	Locality []LocalityReport `json:"locality"`
}

type PCIReport struct {
	Identity string `json:"identity"`
	BARs     []BARReport `json:"bars"`
}

type BARReport struct {
	Index uint8  `json:"index"`
	Kind  string `json:"kind"`
	Base  uint64 `json:"base"`
	Bytes uint64 `json:"bytes"`
}

type APICReport struct {
	Mode string `json:"mode"`
}

type TimerReport struct {
	Label  string `json:"label"`
	Source string `json:"source"`
	PeriodUS uint64 `json:"period_us"`
}

type LocalityReport struct {
	Subject string `json:"subject"`
	Kind    string `json:"kind"`
	Value   string `json:"value"`
	Known   bool   `json:"known"`
}

type RuntimeReport struct {
	Executors       []ExecutorReport       `json:"executors"`
	Placement       []PlacementReport      `json:"placement"`
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
	PayloadType string `json:"payload_type"`
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
