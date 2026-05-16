; Wrela AP trampoline artifact source.
;
; This is the checked-in source for ap_trampoline.bin. The blob is a SIPI-page
; stage-0 trampoline installed at apTrampolineBase (0x8000). Later vCPU startup
; work can patch or extend the metadata slots below; the stage-0 contract here
; is deliberately small, disables interrupts, touches the long-mode control
; registers/MSR path, and falls back to hlt if no later handoff has been wired.
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
;   halt:
;     hlt
;     jmp halt
;
; Metadata slots reserved at the end of the artifact:
;     pml4_phys_low32: dd 0
;     entry_low32:     dd 0
;     stack_low32:     dd 0
