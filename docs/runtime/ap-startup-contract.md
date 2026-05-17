# AP Startup Contract

The bootstrap processor starts application processors through a real-mode SIPI trampoline. The compiler backend validates this embedded trampoline contract before it emits an image.

Required invariants:

- `apTrampolineBase` is `0x8000`.
- The trampoline page range is `[0x8000, 0x9000)`.
- The range is below `0x100000`.
- The range is 4 KiB aligned.
- The range is identity mapped before SIPI.
- The trampoline handoff slots are patched before INIT-SIPI-SIPI.
- The backend copies `_wrela_ap_trampoline_blob` into the trampoline page before AP startup.
- AP ready polling is bounded by `apStartupReadyPollLimit = 10_000_000`.

## Out Of Scope

This contract does not implement high-CR3 trampoline relocation, higher-half AP entry, or hardware-specific calibrated SIPI delays.
