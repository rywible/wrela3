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
	for _, want := range []string{
		"self.panic.fail(code = 0xAC080122)",
		"return NvmeDurabilityMode(mode = NVME_DURABILITY_FUA, requires_flush = false, use_fua = true)",
		"return NvmeDurabilityMode(mode = NVME_DURABILITY_WRITE_PLUS_FLUSH, requires_flush = true, use_fua = false)",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("nvme source missing %q", want)
		}
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
	methodByName(t, directStorage, "identify_namespace")

	source := readRepoFile(t, "wrela/machine/x86_64/nvme.wrela")
	initialize := sourceBetween(t, source, "fn initialize(self, device: PciDevice) -> NvmeDriver {", "\n    fn disable_controller(self)")
	assertOrderedSubstrings(t, initialize, []string{
		"controller.disable_controller()",
		"controller.program_admin_queues()",
		"controller.enable_controller()",
	})
	if strings.Contains(source, "fn identify_controller(self)") {
		t.Fatalf("NvmeDriver must not retain stub identify_controller")
	}
	driverBody := sourceBetween(t, source, "unique driver NvmeDriver {", "\n    fn foreground_storage_path")
	if strings.Contains(driverBody, "fn identify_namespace(self") {
		t.Fatalf("NvmeDriver must not retain stub identify_namespace")
	}
	directIdentify := sourceBetween(t, source, "fn identify_namespace(self, namespace_id: U32) -> NvmeNamespace {", "\n    fn create_io_completion_queue")
	assertOrderedSubstrings(t, directIdentify, []string{
		"self.queues.data_buffer.zero()",
		"self.submit_admin_command(",
		"opcode = NVME_ADMIN_OPCODE_IDENTIFY",
		"self.poll_admin_completion(command_id = identify.command_id)",
		"self.queues.data_buffer.read_u8(offset = 24)",
		"self.queues.data_buffer.read_u64(offset = 0)",
	})
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
		"self.registers.write32(offset = 0x1000 + ((self.u16_to_u32(value = self.queue_id) * 2) * 4), value = self.submission_tail)",
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
	assertMethodSignature(t, methodByName(t, path, "drain_completion_queue"), nil, "NvmeCompletionInterrupt")
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
		"if dword3 == 0",
		"let entry_phase = ((dword3 >> 16) & 1) != 0",
		"if entry_phase != self.expected_phase",
		"return NvmeCompletionInterrupt(queue_id = self.queue_id, completed_count = completed)",
	})
	if strings.Contains(drain, "self.current_entry_phase") {
		t.Fatalf("NvmeCompletionQueue.drain must inspect CQ memory, not a stored phase bit")
	}
	ioDrain := sourceBetween(t, source, "fn drain_completion_queue(self) -> NvmeCompletionInterrupt {", "\n    fn submit_read")
	assertOrderedSubstrings(t, ioDrain, []string{
		"let completion = self.completion_queue.drain()",
		"if completion.completed_count > 0",
		"self.registers.write32(offset = completion_doorbell_offset, value = self.completion_queue.head)",
		"return completion",
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
		"storage.write_first_append()",
		"storage.read_replay_state()",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("nvme event storage fixture missing %q", required)
		}
	}
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
