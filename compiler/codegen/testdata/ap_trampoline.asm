; Wrela AP trampoline artifact source.
;
; This is the checked-in source for ap_trampoline.bin. The blob is a SIPI-page
; stage-0 trampoline installed at apTrampolineBase (0x8000). The BSP patches
; the metadata slots below immediately before INIT/SIPI. The trampoline enters
; long mode using the current identity-map CR3, switches to the patched AP stack,
; marks the ready line, calls the patched executor entry with the patched
; executor context in rdi, and falls back to hlt if that executor returns.
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
;     lgdt [gdt_desc]
;     mov eax, cr4
;     or eax, 0x20
;     mov cr4, eax
;     mov eax, [pml4_phys_low32]
;     mov cr3, eax
;     mov ecx, 0xc0000080
;     rdmsr
;     or eax, 0x100
;     wrmsr
;     mov eax, cr0
;     or eax, 0x80000001
;     mov cr0, eax
;     jmp 0x08:long_mode
;   long_mode:
;     mov ax, 0x10
;     mov ds, ax
;     mov es, ax
;     mov ss, ax
;     mov fs, ax
;     mov gs, ax
;     mov eax, [stack_low32]
;     mov rsp, rax
;     mov edi, [context_low32]
;     mov ebx, [ready_low32]
;     mov qword [rbx], 1
;     mov eax, [entry_low32]
;     call rax
;   halt:
;     hlt
;     jmp halt
;
; Metadata slots:
;     0x7c pml4_phys_low32: dd 0
;     0x80 entry_low32:     dd 0
;     0x84 stack_low32:     dd 0
;     0x88 context_low32:   dd 0
;     0x8c ready_low32:     dd 0
;     0x90 gdt_desc:        dw gdt_end - gdt - 1; dd apTrampolineBase + gdt
