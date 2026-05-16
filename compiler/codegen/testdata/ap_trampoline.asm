; Wrela AP trampoline artifact source.
;
; This is the checked-in source for ap_trampoline.bin. The blob is a SIPI-page
; stage-0 trampoline installed at apTrampolineBase (0x8000). The BSP patches
; the metadata slots below immediately before INIT/SIPI. The trampoline enters
; long mode using the current identity-map CR3, switches to the patched AP stack,
; loads the patched owned IDT, enables the AP local APIC, marks the ready line,
; enables interrupts, calls the patched executor entry with the patched executor
; context in rdi, and falls back to hlt if that executor returns. The AP
; consumes full 64-bit entry/stack/context/ready pointers after entering long
; mode because executor objects may live on a high UEFI stack.
;
; The binary was generated from the byte listing in ap_trampoline.lst:
;
;     xxd -r -p compiler/codegen/testdata/ap_trampoline.lst \
;       compiler/codegen/testdata/ap_trampoline.bin
;
; Register/control intent of the opcode listing:
;     cli
;     cld
;     xor ax, ax
;     mov ds, ax
;     mov es, ax
;     mov ss, ax
;     mov sp, 0x7c00
;     lgdt [apTrampolineBase + gdt_desc]
;     mov eax, cr4
;     or eax, 0x20
;     mov cr4, eax
;     mov eax, [apTrampolineBase + pml4_phys_qword]
;     mov cr3, eax
;     mov ecx, 0xc0000080
;     rdmsr
;     or eax, 0x100
;     wrmsr
;     mov eax, 0x80010033
;     mov cr0, eax
;     jmp 0x08:long_mode
;   long_mode:
;     mov ax, 0x10
;     mov ds, ax
;     mov es, ax
;     mov ss, ax
;     mov fs, ax
;     mov gs, ax
;     lidt [idt_desc]
;     mov rax, [stack_qword]
;     mov rsp, rax
;     mov rdi, [context_qword]
;     mov rbx, [ready_qword]
;     mov r11d, 0xfee00000
;     mov eax, 0x1ff
;     mov dword [r11 + 0xf0], eax
;     mov dword [rbx], 1
;     sti
;     mov rax, [entry_qword]
;     call rax
;   halt:
;     hlt
;     jmp halt
;
; Metadata slots:
;     0xa0 pml4_phys_qword: dq 0
;     0xa8 entry_qword:     dq 0
;     0xb0 stack_qword:     dq 0
;     0xb8 context_qword:   dq 0
;     0xc0 ready_qword:     dq 0
;     0xc8 gdt_desc:        dw gdt_end - gdt - 1; dd apTrampolineBase + gdt
;     0xe8 idt_desc:        dw 0; dq 0, patched from the BSP IDTR
