package sem

import (
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/nvmefmt"
)

func TestNvmeSourceCompiles(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("build index diagnostics: %#v", ds)
	}
	checked, ds := Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	driver := moduleType(t, checked.Index, "machine.x86_64.nvme", "NvmeDriver")
	initialize := methodByName(t, driver, "initialize")
	assertMethodSignature(t, initialize, []string{"device:PciDevice"}, "NvmeDriver")
	claimController := methodByName(t, driver, "claim_controller")
	assertMethodSignature(t, claimController, []string{"devices:PciDeviceSet", "occurrence:U64"}, "PciDevice")

	path := moduleType(t, checked.Index, "machine.x86_64.nvme", "NvmeIoPath")
	if path.Kind != KindDriverPath {
		t.Fatalf("NvmeIoPath kind = %s, want driver path", path.Kind)
	}
	kind, eventType, symbol := pathRouteMetadata(path)
	if kind != "nvme_completion" || eventType != "machine.x86_64.nvme.NvmeCompletionInterrupt" || symbol != "_wrela_event_fn_machine_x86_64_nvme_NvmeIoPath_interrupt" {
		t.Fatalf("NvmeIoPath route metadata = %q, %q, %q", kind, eventType, symbol)
	}
	event := checked.Index.InterruptEvent("machine.x86_64.nvme", "NvmeIoPath")
	if event == nil || event.EventType.Name != "NvmeCompletionInterrupt" {
		t.Fatalf("NvmeIoPath interrupt event = %#v, want NvmeCompletionInterrupt", event)
	}

	namespace := moduleType(t, checked.Index, "machine.x86_64.nvme", "NvmeNamespace")
	assertTypeFields(t, namespace, map[string]string{
		"logical_block_size":                  "U64",
		"supports_zns":                        "Bool",
		"supports_fua":                        "Bool",
		"atomic_write_unit_blocks":            "U32",
		"power_fail_atomic_write_unit_blocks": "U32",
		"volatile_write_cache":                "Bool",
	})

	source := readRepoFile(t, "wrela/machine/x86_64/nvme.wrela")
	for _, want := range []string{
		"const NVME_CLASS_MASS_STORAGE: U64 = 0x01",
		"const NVME_SUBCLASS_NVM: U64 = 0x08",
		"const NVME_PROGIF_EXPRESS: U64 = 0x02",
		"candidate.identity.class_code == NVME_CLASS_MASS_STORAGE",
		"candidate.identity.subclass == NVME_SUBCLASS_NVM",
		"candidate.identity.prog_if == NVME_PROGIF_EXPRESS",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("nvme source missing %q", want)
		}
	}
}

func TestNvmeDurabilityMirrorContract(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("build index diagnostics: %#v", ds)
	}
	checked, ds := Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	for _, name := range []string{
		"NVME_LBA_SIZE_512",
		"NVME_LBA_SIZE_4096",
		"NVME_NAMESPACE_CONVENTIONAL",
		"NVME_NAMESPACE_ZNS",
		"NVME_DURABILITY_FUA",
		"NVME_DURABILITY_PFAIL_ATOMIC_FUA",
		"NVME_DURABILITY_WRITE_PLUS_FLUSH",
	} {
		if _, ok := checked.Index.LookupConst("machine.x86_64.nvme", name); !ok {
			t.Fatalf("missing nvme constant %s", name)
		}
	}

	mode := moduleType(t, checked.Index, "machine.x86_64.nvme", "NvmeDurabilityMode")
	assertTypeFields(t, mode, map[string]string{
		"requires_flush": "Bool",
		"use_fua":        "Bool",
	})

	selector := moduleType(t, checked.Index, "machine.x86_64.nvme", "NvmeDurabilitySelector")
	choose := methodByName(t, selector, "choose")
	assertMethodSignature(t, choose, []string{"namespace:NvmeNamespace", "target_batch_blocks:U32"}, "NvmeDurabilityMode")

	source := readRepoFile(t, "wrela/machine/x86_64/nvme.wrela")
	directStorage := moduleType(t, checked.Index, "machine.x86_64.nvme", "NvmeDirectStorage")
	assertMethodSignature(t, methodByName(t, directStorage, "first_append_durability_mode"), nil, "NvmeDurabilityMode")
	assertMethodSignature(t, methodByName(t, directStorage, "first_append_durability_mode_value"), nil, "U64")
	for _, want := range []string{
		"self.panic.fail(code = 0xAC080122)",
		"return NvmeDurabilityMode(mode = NVME_DURABILITY_FUA, requires_flush = false, use_fua = true)",
		"return NvmeDurabilityMode(mode = NVME_DURABILITY_WRITE_PLUS_FLUSH, requires_flush = true, use_fua = false)",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("nvme source missing %q", want)
		}
	}
	writeFirstAppend := sourceBetween(t, source, "fn write_first_append(self, foreground: ForegroundStoragePath, completions: TopicSubscription<NvmeCompletionInterrupt>) -> NvmeDurableWriteResult {", "\n    fn prepare_first_append_events")
	assertOrderedSubstrings(t, writeFirstAppend, []string{
		"let durability = self.first_append_durability_mode()",
		"self.submit_io_write_blocks(",
		"fua = durability.use_fua",
		"self.wait_io_completion_interrupt(command_id = write.command_id, completions = completions)",
		"if durability.requires_flush",
		"self.submit_io_flush(namespace_id = self.namespace.namespace_id)",
		"self.wait_io_completion_interrupt(command_id = flush.command_id, completions = completions)",
		"self.submit_io_read_blocks(",
		"self.wait_io_completion_interrupt(command_id = verify.command_id, completions = completions)",
		"return NvmeDurableWriteResult(",
	})
	if strings.Contains(writeFirstAppend, "fua = false") {
		t.Fatalf("write_first_append must use selected durability, not hard-code fua=false")
	}
	if strings.Contains(writeFirstAppend, "self.poll_io_completion") {
		t.Fatalf("write_first_append must consume NVMe completion events, not poll IO completions")
	}
	waitIO := sourceBetween(t, source, "fn wait_io_completion_interrupt(self, command_id: U16, completions: TopicSubscription<NvmeCompletionInterrupt>) {", "\n    fn resynchronize_io_completion_after_arm")
	assertOrderedSubstrings(t, waitIO, []string{
		"self.disable_cpu_interrupts()",
		"match completions.try_next()",
		"completions.arm_wait()",
		"self.resynchronize_io_completion_after_arm(command_id = command_id)",
		"self.wait_for_interrupt_window()",
		"match completions.try_next()",
		"Option.Some(value = completed)",
		"completed.queue_id != 1",
		"completed.completed_count > 0",
		"self.consume_io_completion_after_interrupt(command_id = command_id)",
		"self.enable_cpu_interrupts()",
		"self.panic.fail(code = 0xAC080137)",
	})
	if strings.Contains(waitIO, "while") && strings.Contains(waitIO, "self.consume_io_completion(command_id = command_id)") {
		t.Fatalf("direct IO wait must not poll the CQ before a routed interrupt event")
	}
	resync := sourceBetween(t, source, "fn resynchronize_io_completion_after_arm(self, command_id: U16) -> Bool {", "\n    fn consume_io_completion_after_interrupt")
	assertOrderedSubstrings(t, resync, []string{
		"return self.consume_io_completion(command_id = command_id)",
	})
	afterIOInterrupt := sourceBetween(t, source, "fn consume_io_completion_after_interrupt(self, command_id: U16) -> Bool {", "\n    fn consume_io_completion")
	assertOrderedSubstrings(t, afterIOInterrupt, []string{
		"while attempts < NVME_COMPLETION_TIMEOUT_POLLS",
		"if self.consume_io_completion(command_id = command_id)",
		"self.wait_for_poll_window()",
		"return false",
	})
	if strings.Contains(afterIOInterrupt, "self.wait_for_interrupt_window()") {
		t.Fatalf("post-interrupt CQ visibility wait must not block waiting for another interrupt")
	}
	for _, want := range []string{"asm fn wait_for_completion_window(self)", "pause", "asm fn wait_for_interrupt_window(self)", "sti", "hlt", "cli", "asm fn enable_cpu_interrupts(self)"} {
		if !strings.Contains(source, want) {
			t.Fatalf("nvme interrupt wait source missing %q", want)
		}
	}
	consumeIO := sourceBetween(t, source, "fn consume_io_completion(self, command_id: U16) -> Bool {", "\n    asm fn wait_for_completion_window")
	assertOrderedSubstrings(t, consumeIO, []string{
		"self.completion_ready(dword3 = dword3, expected_phase = self.io_phase)",
		"self.io_completion_for_command(dword3 = dword3, command_id = command_id)",
		"self.validate_completion(dword3 = dword3, command_id = command_id)",
		"self.advance_completion_head(head = self.io_head, depth = NVME_FOREGROUND_IO_QUEUE_DEPTH)",
	})
	for _, forbidden := range []string{
		"find_io_completion_index",
		"io_phase_known",
		"completion_phase",
		"head = found",
	} {
		if strings.Contains(consumeIO, forbidden) {
			t.Fatalf("direct IO completion must consume only the tracked CQ head; found %q", forbidden)
		}
	}
	if strings.Contains(consumeIO, "self.io_phase = ((dword3 >> 16) & 1) != 0") ||
		strings.Contains(consumeIO, "self.io_phase = ((found_dword3 >> 16) & 1) != 0") {
		t.Fatalf("direct IO completion must keep tracked phase state instead of learning it from each CQE")
	}
}

func TestNvmeInitMirrorContract(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("build index diagnostics: %#v", ds)
	}
	checked, ds := Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	driver := moduleType(t, checked.Index, "machine.x86_64.nvme", "NvmeDriver")
	for _, name := range []string{
		"disable_controller",
		"program_admin_queues",
		"enable_controller",
	} {
		methodByName(t, driver, name)
	}
	directStorage := moduleType(t, checked.Index, "machine.x86_64.nvme", "NvmeDirectStorage")
	methodByName(t, directStorage, "identify_controller")
	methodByName(t, directStorage, "identify_namespace")

	source := readRepoFile(t, "wrela/machine/x86_64/nvme.wrela")
	for name, want := range map[string]uint64{
		"NVME_ADMIN_QUEUE_DEPTH":         32,
		"NVME_FOREGROUND_IO_QUEUE_DEPTH": 256,
		"NVME_BACKGROUND_IO_QUEUE_DEPTH": 128,
	} {
		if got := checked.Index.ConstValue("machine.x86_64.nvme", name); got != want {
			t.Fatalf("%s = %d, want %d", name, got, want)
		}
	}
	initialize := sourceBetween(t, source, "fn initialize(self, device: PciDevice) -> NvmeDriver {", "\n    fn disable_controller(self)")
	assertOrderedSubstrings(t, initialize, []string{
		"controller.disable_controller()",
		"controller.program_admin_queues()",
		"controller.enable_controller()",
	})
	driverBody := sourceBetween(t, source, "unique driver NvmeDriver {", "\n    fn foreground_storage_path")
	if strings.Contains(driverBody, "fn identify_controller(self)") {
		t.Fatalf("NvmeDriver must not retain stub identify_controller")
	}
	if strings.Contains(driverBody, "fn identify_namespace(self") {
		t.Fatalf("NvmeDriver must not retain stub identify_namespace")
	}
	directController := sourceBetween(t, source, "fn identify_controller(self) -> NvmeControllerFacts {", "\n    fn identify_namespace")
	for _, want := range []string{
		"cdw10 = 1",
		"let vwc = (self.queues.data_buffer.read_u8(offset = 256) & 1) != 0",
		"self.queues.data_buffer.read_u16(offset = 512)",
		"self.queues.data_buffer.read_u16(offset = 514)",
		"supports_fua = true",
	} {
		if !strings.Contains(directController, want) {
			t.Fatalf("Identify Controller source missing %q", want)
		}
	}
	directIdentify := sourceBetween(t, source, "fn identify_namespace(self, namespace_id: U32) -> NvmeNamespace {", "\n    fn create_io_completion_queue")
	assertOrderedSubstrings(t, directIdentify, []string{
		"let controller = self.identify_controller()",
		"self.queues.data_buffer.zero()",
		"self.submit_admin_command(",
		"opcode = NVME_ADMIN_OPCODE_IDENTIFY",
		"self.wait_admin_completion_interrupt(command_id = identify.command_id)",
		"let format = self.queues.data_buffer.read_u8(offset = 26) & 0x0F",
		"self.queues.data_buffer.read_u64(offset = 0)",
		"supports_fua = controller.supports_fua",
		"atomic_write_unit_blocks = controller.atomic_write_unit_blocks",
		"power_fail_atomic_write_unit_blocks = controller.power_fail_atomic_write_unit_blocks",
		"volatile_write_cache = controller.volatile_write_cache",
	})
	if strings.Contains(directIdentify, "read_u8(offset = 24)") {
		t.Fatalf("Identify Namespace FLBAS must use NVMe byte offset 26")
	}
	waitAdmin := sourceBetween(t, source, "fn wait_admin_completion_interrupt(self, command_id: U16) {", "\n    fn consume_admin_completion")
	assertOrderedSubstrings(t, waitAdmin, []string{
		"self.disable_cpu_interrupts()",
		"if self.consume_admin_completion(command_id = command_id)",
		"self.wait_for_poll_window()",
		"self.consume_admin_completion(command_id = command_id)",
		"self.panic.fail(code = 0xAC080136)",
	})
	consumeAdmin := sourceBetween(t, source, "fn consume_admin_completion(self, command_id: U16) -> Bool {", "\n    fn wait_io_completion_interrupt")
	assertOrderedSubstrings(t, consumeAdmin, []string{
		"self.completion_ready(dword3 = dword3, expected_phase = self.admin_phase)",
		"self.admin_completion_for_command(dword3 = dword3, command_id = command_id)",
		"self.validate_completion(dword3 = dword3, command_id = command_id)",
		"self.advance_completion_head(head = self.admin_head, depth = NVME_ADMIN_QUEUE_DEPTH)",
		"self.cq_doorbell_offset(queue_id = 0)",
	})
	for _, forbidden := range []string{"poll_admin_completion", "find_admin_completion_index"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("admin command completions must be interrupt-waited without polling fallback; found %q", forbidden)
		}
	}
	for _, forbidden := range []string{"while scan", "while grace"} {
		if strings.Contains(waitAdmin, forbidden) || strings.Contains(consumeAdmin, forbidden) {
			t.Fatalf("admin command completions must be interrupt-waited without polling fallback; found %q", forbidden)
		}
	}
	directInit := sourceBetween(t, source, "fn initialize(self) -> NvmeDirectStorage {", "\n    fn disable_controller(self)")
	assertOrderedSubstrings(t, directInit, []string{
		"self.create_io_completion_queue(queue_id = 1",
		"depth = NVME_FOREGROUND_IO_QUEUE_DEPTH",
		"interrupt_entry = self.foreground_interrupt_entry",
		"self.create_io_submission_queue(queue_id = 1",
		"depth = NVME_FOREGROUND_IO_QUEUE_DEPTH",
		"self.create_io_completion_queue(queue_id = 2",
		"depth = NVME_BACKGROUND_IO_QUEUE_DEPTH",
		"interrupt_entry = self.background_interrupt_entry",
		"self.create_io_submission_queue(queue_id = 2",
		"depth = NVME_BACKGROUND_IO_QUEUE_DEPTH",
	})
	createCQ := sourceBetween(t, source, "fn create_io_completion_queue(", "\n    fn create_io_submission_queue")
	assertOrderedSubstrings(t, createCQ, []string{
		"interrupt_entry: U32",
		"let cdw11 = 3 | (interrupt_entry * 65536)",
	})
	queueMemory := sourceBetween(t, source, "data NvmeQueueMemory {", "\n}\n\ndata NvmeQueueMemoryBuilder")
	for _, want := range []string{"foreground_io_sq", "foreground_io_cq", "background_io_sq", "background_io_cq"} {
		if !strings.Contains(queueMemory, want) {
			t.Fatalf("NvmeQueueMemory missing %s", want)
		}
	}
	for _, want := range []string{
		"const NVME_RESET_TIMEOUT_POLLS: U32 = 100000",
		"let reset_timeout = NVME_RESET_TIMEOUT_POLLS",
		"while reset_wait < reset_timeout",
		"const NVME_READY_TIMEOUT_POLLS: U32 = 100000",
		"let ready_timeout = NVME_READY_TIMEOUT_POLLS",
		"while ready_wait < ready_timeout",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("nvme source missing bounded wait shape %q", want)
		}
	}
}

func TestNvmeCommandMirrorContract(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("build index diagnostics: %#v", ds)
	}
	checked, ds := Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	path := moduleType(t, checked.Index, "machine.x86_64.nvme", "NvmeIoPath")
	assertMethodSignature(t, methodByName(t, path, "submit_read"), []string{"namespace_id:U32", "start_lba:U64", "block_count:U32", "prp1:PhysicalAddress"}, "NvmeSubmission")
	assertMethodSignature(t, methodByName(t, path, "submit_write"), []string{"namespace_id:U32", "start_lba:U64", "block_count:U32", "prp1:PhysicalAddress", "fua:Bool"}, "NvmeSubmission")
	assertMethodSignature(t, methodByName(t, path, "submit_flush"), []string{"namespace_id:U32"}, "NvmeSubmission")
	assertMethodSignature(t, methodByName(t, path, "submit_zone_append"), []string{"namespace_id:U32", "start_lba:U64", "block_count:U32", "prp1:PhysicalAddress", "fua:Bool"}, "NvmeSubmission")

	for name, want := range map[string]uint64{
		"NVME_OPCODE_WRITE":       nvmefmt.NVME_OPCODE_WRITE,
		"NVME_OPCODE_READ":        nvmefmt.NVME_OPCODE_READ,
		"NVME_OPCODE_FLUSH":       nvmefmt.NVME_OPCODE_FLUSH,
		"NVME_OPCODE_ZONE_APPEND": nvmefmt.NVME_OPCODE_ZONE_APPEND,
		"NVME_COMMAND_FUA_BIT":    nvmefmt.NVME_COMMAND_FUA_BIT,
	} {
		if got := checked.Index.ConstValue("machine.x86_64.nvme", name); got != want {
			t.Fatalf("%s = %#x, want %#x", name, got, want)
		}
	}

	source := readRepoFile(t, "wrela/machine/x86_64/nvme.wrela")
	for _, want := range []string{
		"return self.submit_data_command(opcode = NVME_OPCODE_READ_U8",
		"return self.submit_data_command(opcode = NVME_OPCODE_WRITE_U8",
		"self.panic.fail(code = 0xAC080183)",
		"self.panic.fail(code = 0xAC080184)",
		"self.completion_queue.panic.fail(code = 0xAC080185)",
		"self.completion_queue.panic.fail(code = 0xAC080186)",
		"self.completion_queue.panic.fail(code = 0xAC080187)",
		"command_dword12 = command_dword12 | 0x40000000",
		"return self.submit_data_command(opcode = NVME_OPCODE_FLUSH_U8",
		"return self.submit_data_command(opcode = NVME_OPCODE_ZONE_APPEND_U8",
		"sqe.write_u8(offset = 0, value = opcode)",
		"sqe.write_u16(offset = 2, value = command_id)",
		"sqe.write_u32(offset = 4, value = namespace_id)",
		"sqe.write_physical_address(offset = 24, value = prp1)",
		"sqe.write_u32(offset = 40, value = self.low32(value = start_lba))",
		"sqe.write_u32(offset = 44, value = self.high32(value = start_lba))",
		"sqe.write_u32(offset = 48, value = command_dword12)",
		"self.registers.write32(offset = self.sq_doorbell_offset(), value = self.submission_tail)",
		"return NvmeSubmission(command_id = command_id)",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("nvme source missing command shape %q", want)
		}
	}
	if strings.Contains(source, "return NvmeSubmission(command_id = 0)") {
		t.Fatalf("nvme submissions must allocate real command ids")
	}
}

func TestNvmeCompletionMirrorContract(t *testing.T) {
	modules := parseUEFIModuleSet(t)
	index, ds := BuildIndex(modules)
	if len(ds) != 0 {
		t.Fatalf("build index diagnostics: %#v", ds)
	}
	checked, ds := Check(index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}

	completionQueue := moduleType(t, checked.Index, "machine.x86_64.nvme", "NvmeCompletionQueue")
	advanceMethod := methodByName(t, completionQueue, "advance")
	if len(advanceMethod.Params) != 2 || advanceMethod.Params[1].Name != "count" || advanceMethod.Params[1].Type.Name != "U32" || advanceMethod.Return != nil {
		t.Fatalf("advance signature = %#v, want count:U32 and no return", advanceMethod)
	}
	assertMethodSignature(t, methodByName(t, completionQueue, "drain"), nil, "NvmeCompletionInterrupt")

	path := moduleType(t, checked.Index, "machine.x86_64.nvme", "NvmeIoPath")
	assertMethodSignature(t, methodByName(t, path, "signal_completion_interrupt"), nil, "NvmeCompletionInterrupt")
	assertMethodSignature(t, methodByName(t, path, "drain_completion_queue_after_interrupt"), []string{"command_id:U16"}, "NvmeCompletionInterrupt")
	assertMethodSignature(t, methodByName(t, path, "drain_completion_queue"), nil, "NvmeCompletionInterrupt")
	assertMethodSignature(t, methodByName(t, path, "wait_for_completion_interrupt"), []string{"completions:TopicSubscription", "command_id:U16"}, "NvmeCompletionInterrupt")
	event := checked.Index.InterruptEvent("machine.x86_64.nvme", "NvmeIoPath")
	if event == nil || event.EventType.Name != "NvmeCompletionInterrupt" {
		t.Fatalf("NvmeIoPath interrupt event = %#v, want NvmeCompletionInterrupt", event)
	}

	source := readRepoFile(t, "wrela/machine/x86_64/nvme.wrela")
	advance := sourceBetween(t, source, "fn advance(self, count: U32) {", "\n    fn drain(self) -> NvmeCompletionInterrupt")
	assertOrderedSubstrings(t, advance, []string{
		"self.head = self.head + 1",
		"if self.head == self.depth",
		"self.head = 0",
		"self.expected_phase = self.expected_phase == false",
	})
	drain := sourceBetween(t, source, "fn drain(self) -> NvmeCompletionInterrupt {", "\n}")
	assertOrderedSubstrings(t, drain, []string{
		"while scanned < self.depth",
		"let cqe_offset = self.u32_to_u64(value = self.head) * 16",
		"let dword3 = self.entries.read_u32(offset = cqe_offset + 12)",
		"let entry_phase = ((dword3 >> 16) & 1) != 0",
		"if entry_phase != self.expected_phase",
		"return NvmeCompletionInterrupt(queue_id = self.queue_id, completed_count = completed, last_command_id = last_command_id, failed = failed)",
	})
	if strings.Contains(drain, "if dword3 == 0") {
		t.Fatalf("completion drain must stop on phase mismatch, not zero DW3")
	}
	if strings.Contains(drain, "self.current_entry_phase") {
		t.Fatalf("NvmeCompletionQueue.drain must inspect CQ memory, not a stored phase bit")
	}
	receiver := sourceBetween(t, source, "interrupt receiver -> NvmeCompletionInterrupt {", "\n    fn signal_completion_interrupt")
	assertOrderedSubstrings(t, receiver, []string{
		"self.registers.write32(offset = NVME_REG_INTMC, value = 0xFFFFFFFF)",
		"return self.signal_completion_interrupt()",
	})
	signal := sourceBetween(t, source, "fn signal_completion_interrupt(self) -> NvmeCompletionInterrupt {", "\n    fn drain_completion_queue")
	assertOrderedSubstrings(t, signal, []string{
		"return NvmeCompletionInterrupt(queue_id = self.queue_id, completed_count = 1, last_command_id = 0, failed = false)",
	})
	afterInterruptDrain := sourceBetween(t, source, "fn drain_completion_queue_after_interrupt(self, command_id: U16) -> NvmeCompletionInterrupt {", "\n    fn drain_completion_queue")
	assertOrderedSubstrings(t, afterInterruptDrain, []string{
		"while attempts < 1024",
		"let completion = self.drain_completion_queue()",
		"if completion.completed_count > 0",
		"completion.last_command_id == self.u16_to_u32(value = command_id)",
		"self.wait_for_poll_window()",
		"return NvmeCompletionInterrupt(queue_id = self.queue_id, completed_count = 0",
	})
	ioDrain := sourceBetween(t, source, "fn drain_completion_queue(self) -> NvmeCompletionInterrupt {", "\n    fn submit_read")
	assertOrderedSubstrings(t, ioDrain, []string{
		"let completion = self.completion_queue.drain()",
		"if completion.completed_count > 0",
		"self.registers.write32(offset = self.cq_doorbell_offset(), value = self.completion_queue.head)",
		"return completion",
	})
	waitTopic := sourceBetween(t, source, "fn wait_for_completion_interrupt(self, completions: TopicSubscription<NvmeCompletionInterrupt>, command_id: U16) -> NvmeCompletionInterrupt {", "\n    asm fn wait_for_completion_window")
	assertOrderedSubstrings(t, waitTopic, []string{
		"match completions.try_next()",
		"completions.arm_wait()",
		"self.wait_for_interrupt_window()",
		"match completions.try_next()",
		"Option.Some(value = completed)",
		"let drained = self.drain_completion_queue_after_interrupt(command_id = command_id)",
		"return drained",
	})
	if strings.Contains(waitTopic, "let fallback = self.drain_completion_queue()") {
		t.Fatalf("path wait must drain only after a routed interrupt event")
	}
}

func TestNvmeDoorbellsAndReservedPaddingSourceContract(t *testing.T) {
	source := readRepoFile(t, "wrela/machine/x86_64/nvme.wrela")
	for _, want := range []string{
		"fn doorbell_stride_bytes(self) -> U32",
		"let dstrd = self.registers.read32(offset = NVME_REG_CAP_HIGH) & 0x0F",
		"return 4 << dstrd",
		"return 0x1000 + (index * self.doorbell_stride_bytes())",
		"fn write_reserved_empty_slot(self, slot_offset: U64, event_id: U64)",
		"fn validate_reserved_empty_slot(self, slot_offset: U64, event_id: U64) -> Bool",
		"fn validate_first_append_reserved_empty_padding(self) -> Bool",
		"fn first_append_reserved_empty_slots(self) -> U64",
		"self.write_reserved_empty_slot(slot_offset = WRELA_STORAGE_EVENT_SLOT_SIZE * 2, event_id = 2)",
		"self.write_reserved_empty_slot(slot_offset = WRELA_STORAGE_EVENT_SLOT_SIZE * 7, event_id = 7)",
		"self.validate_reserved_empty_slot(slot_offset = WRELA_STORAGE_EVENT_SLOT_SIZE * 2, event_id = 2)",
		"self.validate_reserved_empty_slot(slot_offset = WRELA_STORAGE_EVENT_SLOT_SIZE * 7, event_id = 7)",
		"self.submission_depth",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("nvme source missing %q", want)
		}
	}
	if strings.Contains(source, "let doorbell_stride = (cap_high >> 16) & 0x0F") {
		t.Fatalf("doorbell stride must come from CAP.DSTRD bits")
	}
}

func TestNvmeReplaySourceReadsRecoveredFrontier(t *testing.T) {
	source := readRepoFile(t, "wrela/machine/x86_64/nvme.wrela")
	replay := sourceBetween(t, source, "fn read_replay_state(self, foreground: ForegroundStoragePath, completions: TopicSubscription<NvmeCompletionInterrupt>) -> NvmeReplayState {", "\n    fn first_append_durability_mode")
	assertOrderedSubstrings(t, replay, []string{
		"self.submit_io_read_blocks(",
		"self.wait_io_completion_interrupt(command_id = read.command_id, completions = completions)",
		"self.validate_first_append_slot(slot_offset = 0",
		"self.validate_first_append_slot(slot_offset = WRELA_STORAGE_EVENT_SLOT_SIZE",
		"self.validate_first_append_orphan_payload()",
		"let recovered_last_event_id = self.queues.data_buffer.read_u64(offset = WRELA_STORAGE_EVENT_SLOT_SIZE)",
		"last_event_id = recovered_last_event_id",
		"projection_watermark = recovered_last_event_id",
	})
	if strings.Contains(replay, "last_event_id = 1") || strings.Contains(replay, "projection_watermark = 1") {
		t.Fatalf("read_replay_state must return the recovered frontier read from storage, not hard-coded replay counters")
	}
	if strings.Contains(replay, "self.poll_io_completion") {
		t.Fatalf("read_replay_state must consume NVMe completion events, not poll IO completions")
	}
}

func TestNvmeEventStorageProgramReclaimsRejectedRelocationExtent(t *testing.T) {
	source := readRepoFile(t, "tests/e2e/fixtures/nvme_event_storage/program.wrela")
	replay := sourceBetween(t, source, "fn replay_outcome(", "        return ReplayOutcome")
	assertOrderedSubstrings(t, replay, []string{
		"let facts = self.blob_facts(scratch = scratch)",
		"let old_ref = BlobRef(blob_id = facts.acknowledged_blob_id",
		"let new_ref = BlobRef(blob_id = facts.relocated_blob_id",
		"let relocation_accepted = truth.can_accept_relocate(proposal = relocation)",
		"allocated_extent_count = 3",
		"acknowledged_ref_count = 1",
		"if relocation_accepted == false",
		"if reclaimed.extent_count == 2",
		"let first_reclaimed = reclaimed.extents.get(index = 0)",
		"let second_reclaimed = reclaimed.extents.get(index = 1)",
		"if first_reclaimed.start_lba == facts.relocated_start_lba",
		"if second_reclaimed.start_lba == facts.orphan_start_lba",
	})
	blobFacts := sourceBetween(t, source, "fn blob_facts(self, scratch: ReplayScratch) -> ReplayBlobFacts {", "\n}")
	assertOrderedSubstrings(t, blobFacts, []string{
		"let acknowledged = scratch.acknowledged_refs.get(index = 0)",
		"let relocated = scratch.allocated_extents.get(index = 1)",
		"let orphan = scratch.allocated_extents.get(index = 2)",
		"relocated_start_lba = relocated.start_lba",
		"orphan_start_lba = orphan.start_lba",
	})
}

func TestNvmeEventStorageFixtureUsesDirectNvmeStorage(t *testing.T) {
	source := readRepoFile(t, "tests/e2e/fixtures/nvme_event_storage/main.wrela")
	for _, forbidden := range []string{
		"platform.uefi.block_io",
		"UefiStorageSentinel",
		"UefiBlockIoBootServices",
		"read_blocks",
		"write_blocks",
		"NVME_DEBUG",
		"require_device(vendor_id",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("nvme event storage fixture must not use %q", forbidden)
		}
	}
	for _, required := range []string{
		"require_class(class_code = 0x01, subclass = 0x08, prog_if = 0x02, occurrence = 0)",
		"claim_mmio_bar_at32(index = 0, base = 0xC0000000)",
		"NvmeDirectStorage(",
		"event_slots_reserved_empty = storage.first_append_reserved_empty_slots()",
		"selected_durability_mode = storage.first_append_durability_mode_value()",
		"storage.write_first_append(foreground = foreground, completions = completions)",
		"storage.read_replay_state(foreground = foreground, completions = nvme_foreground_completion_subscription)",
		"nvme_foreground_interrupt_topic.subscribe(subscriber = foreground_slot)",
		"route_nvme_io_completion_interrupts(",
		"interrupt_routes = nvme_interrupt_routes",
		"owned_nvme_interrupt_routes = hardware.hardware_plan.pci.nvme_interrupt_routes.reapply_msix_table(",
		"device = hardware.hardware_plan.pci.nvme_device",
		"foreground_interrupt_entry = owned_nvme_interrupt_routes.foreground_entry",
		"background_interrupt_entry = owned_nvme_interrupt_routes.background_entry",
		"hardware.hardware_plan.pci.nvme_device.enable_mmio_and_bus_master_mmio()",
		"vector = owned_nvme_interrupt_routes.foreground_vector",
		"vector = owned_nvme_interrupt_routes.background_vector",
		"foreground_path_vector = owned_nvme_interrupt_routes.foreground_vector",
		"background_path_vector = owned_nvme_interrupt_routes.background_vector",
		"ApicInterruptController(local_apic = hardware.hardware_plan.interrupts.local_apic)",
		"interrupt_controller.local_apic.enable()",
		"interrupt_controller.enable_cpu_interrupts()",
		"CoreSpscProducer<CommittedAtomicGroup>",
		"CoreSpscConsumer<CommittedAtomicGroup>",
		"committed_groups = committed_group_producer",
		"NvmeAppendReporter(serial_path = serial_path, memory = foreground_memory).report(writer = writer, storage = storage, foreground = foreground, completions = nvme_foreground_completion_subscription)",
		"pending_write_count = durable.pending_write_count",
		"completed_write_count = durable.completed_write_count",
		"writer.publish_committed_group(token = token)",
		"NvmeStorageMaintenance(",
		"self.committed_groups.try_next()",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("nvme event storage fixture missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"let nvme_msix = nvme_device.claim_msix_in_bar",
		"nvme_msix.route_entry",
		"interrupt_entry = 1)",
		"vector = 80",
		"vector = 81",
		"foreground_path_vector = 80",
		"background_path_vector = 81",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("nvme event storage fixture must not hard-code MSI-X-only routing with %q", forbidden)
		}
	}
}

func TestNvmePciInterruptRoutingSupportsMsiFallback(t *testing.T) {
	source := readRepoFile(t, "wrela/machine/x86_64/pci.wrela")
	for _, want := range []string{
		"data NvmeInterruptRoutes {",
		"foreground_entry: U32",
		"background_entry: U32",
		"foreground_vector: U8",
		"background_vector: U8",
		"msix_capability_offset: U16",
		"fn reapply_msix_table(self, device: PciDevice, table: MmioRegion, target: LocalApic) -> NvmeInterruptRoutes",
		"device.write_config32_mmio(offset = self.msix_capability_offset, value = (header | 0x80000000) & 0xBFFFFFFF)",
		"fn route_nvme_io_completion_interrupts(self, table_bar_index: U8, table: MmioRegion, foreground_vector: InterruptVector, background_vector: InterruptVector, target: LocalApic) -> NvmeInterruptRoutes",
		"if facts.has_msix",
		"self.claim_msix_in_bar_mmio(table_bar_index = table_bar_index, table = table)",
		"msix.route_entry(entry = 0, vector = foreground_vector, target = target)",
		"msix.route_entry(entry = 1, vector = background_vector, target = target)",
		"(header | 0x80000000) & 0xBFFFFFFF",
		"foreground_entry = 0",
		"background_entry = 1",
		"foreground_vector = foreground_vector.value",
		"background_vector = background_vector.value",
		"msix_capability_offset = msix.capability_offset",
		"if facts.has_msi",
		"self.claim_msi()",
		"msi.route_pair(foreground_vector = foreground_vector, background_vector = background_vector, target = target)",
		"foreground_entry = 0",
		"background_entry = 1",
		"background_vector = background_vector.value",
		"msix_capability_offset = 0",
		"fn route_pair(self, foreground_vector: InterruptVector, background_vector: InterruptVector, target: LocalApic) -> PciInterruptRoute",
		"if (control & 0x0E) == 0",
		"background_vector.value != foreground_vector.value + 1",
		"(header & 0xFF8FFFFF) | 0x00110000",
		"self.panic.fail(code = 0xAC060024)",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("PCI NVMe interrupt routing source missing %q", want)
		}
	}
	routing := sourceBetween(t, source, "fn route_nvme_io_completion_interrupts", "\n\ndata MsiCapability")
	if strings.Contains(routing, "self.claim_msix(table_bar_index") {
		t.Fatalf("NVMe MSI-X routing helper must reuse the relocated BAR instead of claiming BAR0 again")
	}
	msiFallback := sourceBetween(t, routing, "if facts.has_msi", "self.panic.fail")
	assertOrderedSubstrings(t, msiFallback, []string{
		"let foreground_route = msi.route_pair(foreground_vector = foreground_vector, background_vector = background_vector, target = target)",
		"let background_route = PciInterruptRoute(vector = background_vector)",
		"foreground_vector = foreground_vector.value",
		"background_vector = background_vector.value",
		"foreground_route = foreground_route",
		"background_route = background_route",
	})
}

func TestLocalApicEnableClearsTaskPriorityForNvmeMsiX(t *testing.T) {
	source := readRepoFile(t, "wrela/machine/x86_64/interrupts.wrela")
	enable := sourceBetween(t, source, "fn enable(self) {", "\n    fn eoi")
	assertOrderedSubstrings(t, enable, []string{
		"self.mmio().write32(offset = 0x80, value = 0)",
		"self.mmio().write32(offset = 0xF0, value = 0x1FF)",
	})
}

func sourceBetween(t *testing.T, source string, start string, end string) string {
	t.Helper()
	startIndex := strings.Index(source, start)
	if startIndex < 0 {
		t.Fatalf("source missing start %q", start)
	}
	rest := source[startIndex:]
	endIndex := strings.Index(rest, end)
	if endIndex < 0 {
		t.Fatalf("source missing end %q", end)
	}
	return rest[:endIndex]
}

func assertOrderedSubstrings(t *testing.T, source string, wants []string) {
	t.Helper()
	offset := 0
	for _, want := range wants {
		index := strings.Index(source[offset:], want)
		if index < 0 {
			t.Fatalf("source missing %q after byte offset %d", want, offset)
		}
		offset += index + len(want)
	}
}

func assertMethodSignature(t *testing.T, method *Method, params []string, returnType string) {
	t.Helper()
	gotParams := method.Params
	if len(gotParams) > 0 && gotParams[0].Name == "self" {
		gotParams = gotParams[1:]
	}
	if len(gotParams) != len(params) {
		t.Fatalf("%s params = %#v, want %#v", method.Name, method.Params, params)
	}
	for i, want := range params {
		got := gotParams[i].Name + ":" + gotParams[i].Type.Name
		if got != want {
			t.Fatalf("%s param %d = %s, want %s", method.Name, i, got, want)
		}
	}
	if method.Return == nil || method.Return.Name != returnType {
		t.Fatalf("%s return = %#v, want %s", method.Name, method.Return, returnType)
	}
}
