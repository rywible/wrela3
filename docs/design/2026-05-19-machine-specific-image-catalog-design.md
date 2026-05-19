# Machine-Specific Image Catalog Design

## Purpose

Wrela should start its real-hardware product shape with a catalog of concrete,
machine-specific images.

The goal is not to make one generic image for every PC. The goal is to make each
image a clear composition root for one machine type:

```text
images/
  qemu_q35/main.wrela
  gmktech_v1002/main.wrela
  beelink_eq13/main.wrela
  protectli_vp2420/main.wrela
```

Each `main.wrela` is its own image. It may import different drivers, claim
different devices, choose different memory policy, wire different executors, and
encode different firmware expectations.

This fits Wrela's image-first thesis: the machine is part of the program.

## Current Context

Wrela already has the lower-level direction needed for this:

- UEFI boot into generated x86_64 PE/COFF EFI images
- source-visible UEFI configuration tables and memory maps
- ACPI RSDP/RSDT/XSDT lookup
- MADT CPU, LAPIC, IOAPIC, and interrupt override parsing
- MCFG and PCIe ECAM discovery
- PCI bus/device/function scanning, including PCI bridges
- PCI BAR, MSI, and MSI-X claim paths
- explicit memory, interrupt, timer, executor, and driver authorities

That substrate is becoming generic at the platform layer. The current image
shape is still example-specific: the top-level examples wire QEMU devices such
as EDU and ivshmem directly.

This design keeps the generic work at the lower layers and makes the top-level
image layer intentionally concrete.

## Core Decision

Wrela should build a catalog of roughly ten concrete images before trying to
generalize the top-level image shape.

The catalog is allowed to repeat code.

Do not start by creating:

- a broad capability-class image
- a hidden compiler hardware selector
- a large `machines.*` abstraction layer
- a universal driver registry
- a generic "PC appliance" image

Instead, create concrete image roots:

```text
images/gmktech_v1002/main.wrela
images/qemu_q35/main.wrela
images/protectli_vp2420/main.wrela
```

Each image should say exactly what it wants from the hardware. If ten images
repeat the same shape, then Wrela can extract that shape after seeing real
evidence.

## Design Principles

### Let The Image Be Literal

A machine-specific image should be easy to read as a whole-machine wiring file.

It is acceptable for `gmktech_v1002/main.wrela` to directly require a known NIC,
known storage controller, known CPU count, known interrupt route shape, known
debug console policy, and known memory layout policy.

The top-level image should not hide that behind a premature helper merely to
look clean.

### Deduplicate Below The Image

Shared code belongs in lower layers:

```text
wrela/platform/uefi/
wrela/platform/acpi/
wrela/platform/hardware/
wrela/machine/x86_64/
wrela/drivers/
```

Those layers should keep growing as reusable substrate and driver code.

Top-level image roots can stay repetitive until the repetition becomes obviously
real.

### Harvest Abstractions From The Catalog

Abstractions should be extracted only after repeated concrete images show the
same pattern.

Good reasons to extract:

- three or more images select NVMe the same way
- several mini PCs route serial/debug output the same way
- several Realtek NIC images repeat identical initialization
- several firmware families need the same ACPI workaround
- several images allocate executor memory with the same topology policy

Bad reasons to extract:

- the first image looks a little long
- a helper might be useful someday
- the design wants a symmetric taxonomy
- two images happen to share one line

The catalog should teach the abstractions, not the other way around.

### Keep Discovery Source-Visible

Machine-specific does not mean opaque.

An image may be built for one machine type, but it should still discover and
validate the boot hardware at runtime:

```wrela
let discovery = PlatformDiscoveryRoot(panic = BootPanic()).from_uefi(hardware = hardware)
let nic = discovery.pci.require_device(vendor_id = 0x10EC, device_id = 0x8125, occurrence = 0)
let nvme = discovery.pci.require_class(class_code = 0x01, subclass = 0x08, occurrence = 0)
```

If the required hardware is absent or changed, the image should fail loudly
through the normal boot-fatal path.

### Prefer Explicit Contracts Over Inference First

Every catalog image should have an explicit hardware contract. The compiler can
later infer more of this from source, but the first version should make the
contract visible and reviewable.

The contract is the label on the shelf. The `main.wrela` file remains the
source of the image.

## Repository Shape

The catalog should be organized around image roots, not machine profile objects:

```text
images/
  qemu_q35/
    main.wrela
    contract.wrela
    README.md
  gmktech_v1002/
    main.wrela
    contract.wrela
    README.md
  protectli_vp2420/
    main.wrela
    contract.wrela
    README.md

wrela/
  platform/
  machine/
  drivers/
```

`main.wrela` owns the image composition. `contract.wrela` or an equivalent
source-visible declaration owns matching metadata for installers and tools.

The exact file name can change, but the boundary should stay clear:

- image directory: concrete machine image
- platform modules: generic firmware and hardware discovery
- driver modules: reusable device behavior
- compiler tools: build, contract extraction, probe matching, install support

## Example Image Shape

A future GMKTech image might look like this in spirit:

```wrela
module images.gmktech_v1002.main

use { PlatformDiscoveryRoot } from platform.hardware.discovery
use { BootPanic } from platform.hardware.panic
use { DelegatedHardware } from platform.uefi.transition
use { Realtek8125Driver } from drivers.net.realtek_8125
use { NvmeDriver } from drivers.storage.nvme

image GmkTechV1002 {
    transitions {
        delegated_hardware -> owned_hardware
    }

    phase delegated_hardware(hardware: DelegatedHardware) -> OwnedHardware {
        let panic = BootPanic()
        let discovery = PlatformDiscoveryRoot(panic = panic).from_uefi(hardware = hardware)

        let nic0 = discovery.pci.require_device(
            vendor_id = 0x10EC,
            device_id = 0x8125,
            occurrence = 0
        )
        let nvme0 = discovery.pci.require_class(
            class_code = 0x01,
            subclass = 0x08,
            occurrence = 0
        )

        // Machine-specific memory, interrupt, driver, executor, and app wiring
        // lives here until several images prove a smaller reusable helper.
    }
}
```

The exact driver names and APIs are illustrative. The important point is that
the concrete image root owns the machine-specific choices.

## Image Contracts

Each catalog image should expose a hardware contract that an installer can match
before building or installing the image.

Representative contract fields:

```text
image_name
image_source
target_arch
requires_uefi
requires_acpi
requires_madt
requires_mcfg
min_enabled_cpus
min_memory_bytes
smbios_vendor
smbios_product
pci_devices
pci_classes
required_debug_console
known_firmware_versions
known_quirks
```

Some fields are exact identity matches. Others are capability requirements.

For a concrete machine image, product identity is useful but should not be the
only signal. PCI devices, ACPI features, CPU count, and memory requirements
matter more for safety.

Example conceptual contract:

```text
image: images/gmktech_v1002/main.wrela
arch: x86_64
uefi: required
acpi: required
mcfg: required
cpus: >= 4
memory: >= 4 GiB
smbios:
  vendor: GMKtec
  product: Mini PC V1002
pci:
  required:
    - 10ec:8125
    - class 01:08
```

The compiler should eventually emit a machine-readable form:

```text
build/gmktech_v1002.efi
build/gmktech_v1002.contract.json
build/gmktech_v1002.image-report.json
```

## Distribution Artifacts

Machine-specific images can be distributed as prebuilt artifacts, but the
prebuilt artifact should not be opaque.

A packaged image should include:

```text
gmktech_v1002.efi
gmktech_v1002.contract.json
gmktech_v1002.image-report.json
source-manifest.json
```

The `.efi` is the bootable artifact. The contract tells installers which
machines it expects. The image report explains what the image claims, owns, and
wires. The source manifest ties the artifact back to the exact `main.wrela` and
shared modules used to build it.

This lets Wrela ship convenient machine-specific images without losing the
reviewable source shape that makes the image trustworthy.

## Installer And Image Selection

Users should not have to manually know which image to pick.

The install flow should be:

```text
1. Probe the machine.
2. Match the probe report against catalog contracts.
3. Rank candidate images.
4. Ask the user to confirm the selected image.
5. Install the selected EFI artifact into the EFI System Partition.
```

The probe should ideally run as a tiny UEFI image so it sees the same firmware,
ACPI, memory-map, and PCI world that Wrela images will see.

Host-side probing from Linux or another OS can be useful for convenience, but it
may not see exactly the same boot-time state. UEFI probing should be the source
of truth for installation confidence.

Possible commands:

```sh
wrela probe --out hardware-report.json
wrela match hardware-report.json images/*/*.contract.json
wrela build images/gmktech_v1002/main.wrela -o build/gmktech_v1002.efi
wrela install build/gmktech_v1002.efi --dual-boot
```

Possible user-facing result:

```text
Detected:
  vendor: GMKtec
  product: Mini PC V1002
  uefi: yes
  acpi: yes
  mcfg: yes
  pci:
    10ec:8125 Realtek 2.5GbE
    class 01:08 NVMe controller

Best match:
  images/gmktech_v1002/main.wrela
  confidence: exact

Alternative:
  images/qemu_q35/main.wrela
  confidence: incompatible, missing expected virtual devices
```

The installer must keep image selection separate from storage selection. A
machine can match an image while still requiring careful user confirmation about
which disk, partition, or EFI System Partition should be modified.

## Matching Confidence

The matcher should use tiers rather than a single yes/no:

```text
exact:
  product identity matches and all required hardware facts match

strong:
  product identity is absent or fuzzy, but all required hardware facts match

weak:
  most facts match, but optional or confidence-bearing facts are missing

unsafe:
  any required fact is missing or contradicted
```

Only exact or strong matches should be installable by default.

Weak matches should require explicit expert override.

Unsafe matches should not install.

## Compiler Responsibilities

The compiler should not secretly choose hardware policy while compiling.

Appropriate compiler and tool responsibilities:

- parse image contracts
- emit contract artifacts
- validate that contracts and obvious source requirements agree
- build specific image roots
- produce image reports
- compare probe reports with contracts
- rank candidate images for installers

Inappropriate compiler responsibilities:

- silently changing `main.wrela` based on the current host machine
- hiding hardware discovery decisions inside codegen
- selecting drivers that source did not import or request
- treating host OS hardware state as an implicit build input

If hardware-derived codegen is added later, it should generate explicit Wrela
source or contract files that can be reviewed and committed.

## Hardware-Derived Codegen

The catalog direction does not forbid hardware-derived codegen. It changes its
role.

Instead of:

```text
current machine -> hidden compiler decisions -> .efi
```

prefer:

```text
probe report -> generated starter main.wrela -> reviewed image source -> .efi
```

This is useful for bringing up a new catalog entry. A probe tool could produce a
first draft:

```text
images/new_machine/main.wrela
images/new_machine/contract.wrela
images/new_machine/README.md
```

The generated source should be a starting point, not an opaque permanent layer.

## Relationship To Capability-Class Images

Capability-class images are still valuable, but they are not the first catalog
strategy.

A capability-class image targets a broad envelope such as:

```text
any x86_64 UEFI PC with ACPI, MCFG, NVMe, and a supported NIC
```

A machine-specific catalog image targets a known machine type:

```text
GMKTech V1002 with its expected firmware and device shape
```

Capability-class images require broader driver selection, more fallback paths,
more firmware quirk handling, and a larger hardware test matrix. They should
emerge after concrete machine images reveal common patterns.

The catalog path should produce the raw material for that later step.

## Dedupe Policy

The first ten images should be allowed to repeat top-level wiring.

After that, review repeated blocks and extract only small helpers with obvious
value.

Recommended extraction rule:

```text
Extract only when at least three real images repeat the same behavior and the
helper makes those images easier to audit.
```

Potential future helpers:

- first supported NVMe selection
- Realtek RTL8125 claim and interrupt setup
- Intel i225/i226 claim and interrupt setup
- standard two-executor memory partition
- serial debug console routing
- common APIC/timer initialization
- common UEFI framebuffer selection

The helper should remain source-visible and authority-bearing. It should not
become a hidden runtime registry.

## Dual-Boot Safety

Dual-boot installation should be conservative.

The installer should:

- detect existing EFI System Partitions
- show existing boot entries
- add a Wrela boot entry rather than replacing an existing OS entry
- avoid touching non-EFI OS partitions unless explicitly requested
- make the selected image contract visible before install
- require confirmation for ambiguous storage layouts

Choosing an image and choosing an install target are separate decisions.

## Success Criteria

This design is successful when Wrela can:

- maintain a catalog of concrete machine image roots
- build each image from a normal `main.wrela`
- emit a contract artifact for each image
- probe a machine and rank catalog matches
- install an exact or strong match for dual boot without asking the user to know
  internal hardware IDs
- keep lower-level platform and driver code shared
- defer top-level deduplication until repeated real images justify it

The first milestone should prove this with:

- one QEMU catalog image
- one real-machine catalog image
- matching contracts for both
- a probe report format
- a matcher that can distinguish exact, strong, weak, and unsafe matches

## Open Questions

- Should contracts be declared inside `main.wrela`, in a sibling
  `contract.wrela`, or in compiler-readable metadata generated from Wrela
  source?
- Which real machine should be the first non-QEMU catalog entry?
- Should the first UEFI probe write reports to a FAT filesystem, serial output,
  or both?
- How much SMBIOS identity should be trusted compared with PCI and ACPI facts?
- Should weak matches be buildable but not installable, or blocked entirely by
  default?
