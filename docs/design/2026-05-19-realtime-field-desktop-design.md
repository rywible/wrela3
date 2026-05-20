# Realtime Field Desktop Design

## Purpose

Wrela should grow a beautiful desktop as a first-class appliance image, not as a
traditional operating-system GUI stack layered on top of an opaque compositor.

The desktop should feel like a realtime instrument:

- the display clock is the central pacing authority
- input is sampled as fresh signal
- visible app logic participates in a bounded frame
- every visual object is a field
- rendering is an explicit field execution strategy, not a hidden renderer
- storage, networking, indexing, and other uncertain work cross async
  boundaries instead of blocking the frame
- every missed frame should have an inspectable cause

The first showcase should be a beautiful operating system experience. The goal
is not to emulate an existing desktop stack. The goal is to prove that modern
consumer hardware can run a coherent, field-native, display-paced UI where the
machine shape remains visible from source to frame report.

## Current Context

The current Wrela repository already points in the right direction:

- Wrela images own the whole machine shape instead of assuming an ambient OS.
- Hardware discovery, memory authority, executor placement, interrupt routes,
  topics, and image reports are source-visible.
- Executor placement is explicit. There is no hidden scheduler, migration, or
  work stealing in the current production direction.
- Storage and networking are being designed as dedicated subsystems with typed
  authorities and cross-executor queues.
- The older Wrela field-engine work explored field semantics, support pruning,
  solver portfolios, tile candidates, shared acceleration artifacts, and
  detailed presentation cost reports.

This desktop design extends those ideas into the foreground user experience.
It treats the desktop as one realtime visible world that emits fields and input
targets each frame.

## Assumptions

- The first display path uses UEFI GOP-provided framebuffer modes. A production
  GPU driver is a later milestone.
- Display output is a backend boundary. GOP is the first backend, not the
  permanent display architecture.
- There is no assumed faithful mock AMD APU or Intel integrated GPU. Virtual
  display devices and deterministic framebuffer backends test protocol,
  presentation, replay, and renderer behavior; real AMD and Intel hardware
  validate vendor-specific paths.
- Native AMD and Intel support should begin as display-controller support:
  output discovery, modes, scanout buffers, page flips, vblank/cadence,
  hotplug, cursor planes where available, and memory/cache policy. Full
  render/compute acceleration is a separate later renderer strategy.
- The first guaranteed display target is one monitor. Multiple displays are
  supported opportunistically if firmware exposes multiple GOP handles or later
  display drivers make them available.
- The design target is 1080p internal rendering at 120 presented frames per
  second when hardware scanout supports it.
- If hardware scanout is lower than the renderer's preferred rate, presentation
  matches hardware cadence instead of producing undisplayable frames.
- The first renderer is CPU-first and lane-oriented: AVX2 is the first
  supported path, AVX-512 is a preferred later CPU path, and scalar fallback is
  used for bring-up and tests.
- Every authored visual source is a field. Meshes, user-authored textures, and
  prebaked SDF assets are not source truth for the desktop model.
- Every visible thing is presented as a field. Not every field is analytic:
  photos, video, screenshots, PDFs, camera frames, and remote pixels may enter
  the world as bounded sampled fields with explicit provenance, color space,
  filtering, trust, temporal identity, and cache validity.
- Derived caches, tile bins, layer buffers, glyph coverage tiles, and similar
  artifacts are allowed when they are explicitly derived from fields and carry
  validity.
- The compiler should not own graphics meaning. It may check purity, lifetime,
  placement, boundedness, target features, and authority rules, then lower
  ordinary code.
- The desktop and first apps are Wrela-owned trusted code. The primary model is
  a synchronous realtime frame, not arbitrary third-party ambient app hosting.
- Snapshot boundaries still exist for hidden/background work, untrusted code,
  legacy code, and failure recovery.
- The geometric field model is unified. 2D UI is a constrained planar/surface
  case of the same field model, with specialized 2D execution strategies where
  they are cheaper.
- The first desktop milestone does not require general `async`/`await` syntax.
  Async boundaries are explicit request/completion contracts. Future `await`
  syntax should lower to visible wait sources and must not be allowed in the
  realtime frame lane.

## Non-Goals

This design does not add:

- a POSIX window server
- an HTML/CSS/DOM-style layout engine
- a retained-mode scene graph hidden from Wrela source
- a GPU dependency for the first desktop milestone
- a production AMDGPU, Intel, or NVIDIA driver
- a faithful emulated consumer AMD APU or Intel integrated GPU
- a native Vulkan, OpenGL, or full render/compute GPU stack
- general-purpose process isolation for mutually distrusting desktop apps
- arbitrary third-party GUI app compatibility
- a general coroutine scheduler or `async`/`await` surface for the first
  desktop milestone
- user-authored meshes as the desktop visual primitive
- a texture-backed GUI model where sampled media replaces fields as the desktop
  representation
- backdrop blur, liquid-glass blur, or blur-heavy visual design
- 4K internal rendering at 120fps as a first milestone promise
- a hidden runtime scheduler that rescues slow foreground code by default

The design should not block later GPU drivers, third-party app boundaries,
process isolation, display-server compatibility layers, or 4K+ rendering. Those
should be explicit extensions, not assumptions in the first desktop shape.

## Design Principles

### The Desktop Is One Realtime World

The foreground desktop should be viewed more like a game engine than a
traditional window system. There is one visible world, one display-paced frame
clock, one input sample for the frame, one global field/input framegraph, and
one frame cost report.

Visible apps are participants in that world. They are not opaque foreign
surfaces pasted onto a desktop by a hidden compositor.

### Fields Are Source Truth

Every visual element is expressed as a field:

- desktop background
- panels
- text boxes
- glyphs
- carets
- selections
- scrollbars
- app icons
- windows
- cursor
- shadows
- sampled photos and video frames
- screenshots and remote pixels
- 3D desktop objects
- screensaver geometry

Derived artifacts may accelerate rendering, but they are not the truth. If a
cache, tile table, or layer buffer disagrees with the field source, the field
source wins.

The right rule is:

```text
Everything visible is presented as a field.
Not every field is analytically generated.
```

Sampled content is a field boundary, not an exception to the field world. The
source truth for a photo is sampled media bytes; the source truth for a rounded
panel is analytic field code. Both still carry identity, support, z/order,
provenance, color policy, input/semantic hooks, and frame-report cost.

### The Renderer Is Visible

Wrela should not hide "the renderer" behind declarative UI machinery.

The desktop should provide rendering primitives and one readable reference
field renderer. The renderer is an ordinary low-level program over Wrela data
structures: fields, supports, tile lists, lane packets, clips, blends,
surfaces, and reports.

The source should make these decisions visible:

- how fields are collected
- how supports are computed
- how fields are binned into tiles
- how tiles are evaluated
- how z-order is applied
- how alpha/coverage compositing works
- which caches are used or rejected
- which field execution strategy ran
- when presentation matches scanout cadence

The field language may be lane-abstract, but execution is not implicit. The
image declares the renderer strategies it permits, and every frame report records
the strategy actually used.

### Contracts Are Not Hidden Lowering

A raw function is too opaque for cheap rendering. A field function needs an
explicit low-level contract:

- purity
- bounds or support region
- distance semantics
- dimensionality
- optional repeat/periodicity facts
- optional analytic solve path
- optional cache policy
- optional sampled-source provenance
- lane-abstract evaluation

These contracts do not make rendering declarative. They are the facts the
explicit renderer consumes to avoid sampling the universe every frame.

### Display Deadlines Are Design Constraints

Foreground visible code lives under the display deadline. If a visible app
misses the frame budget because it performs unbounded work, that is a bug in
the app or in the frame design.

The system should make that bug obvious through reports instead of hiding it
behind an ambient scheduler.

### Async Work Crosses Explicit Boundaries

Storage, networking, indexing, compilation, large parsing jobs, thumbnails,
AI calls, and other uncertain work do not run in the frame lane. They run on
dedicated executors and return results through bounded queues, completion
streams, tickets, messages, or snapshots.

The frame lane may submit work and poll completed work. It must not wait or
await.

## Core Data Model

The renderer consumes explicit field records. The exact syntax will evolve with
the language, but the shape should remain simple and backend-neutral.

Field source should describe meaning, not AVX width. Authored field functions
are lane-abstract: they describe evaluation for one logical sample, and an
explicit renderer strategy chooses whether that runs as scalar code, AVX2
packets, AVX-512 packets, GPU lanes, or another future backend.

Representative unified field shape:

```wrela
data Field {
    identity: FieldIdentity
    z: I32
    support: FieldSupport
    semantics: DistanceSemantics
    clip: Clip
    cache: CachePolicy
    source: FieldSource
    temporal: TemporalIdentity
    data: FieldData
    eval: pure lane fn(FieldData, SamplePoint<Lane>) -> FieldValue<Lane>
    shade: pure lane fn(FieldData, FieldValue<Lane>, SamplePoint<Lane>) -> Color<Lane>
}
```

The `lane` marker is not hidden magic. It means the function is legal to run
over a backend-selected lane group. The image still declares the allowed
execution strategies, and the frame report names the strategy and lane width
used for each renderer path.

Representative distance semantics:

```wrela
enum DistanceSemantics {
    ExactSignedDistance
    ConservativeLowerBound
    CoverageOnly
    Opaque
}
```

Representative support shape:

```wrela
enum FieldSupport {
    Empty
    Rect(rect: Rect)
    RoundedRect(rect: Rect, radius: F32)
    Circle(center: Vec2, radius: F32)
    Plane(surface: SurfaceFrame, shape: SurfaceSupport)
    Bounds3D(bounds: Bounds3D)
    PeriodicGrid(bounds: Rect, cell: Vec2)
    Unknown
}
```

`SurfaceSupport` is the planar subset used by surface-bound UI fields:
rectangles, rounded rectangles, circles, text glyph bounds, and other cheap
2D supports.

`Unknown` is allowed but expensive. It should be visible in reports and should
not be common in foreground UI. Foreground frame budgets may cap or reject
unknown supports for visible app work.

Representative source shape:

```wrela
enum FieldSource {
    Analytic
    Sampled(content: SampledContent)
    DerivedCache(parent: FieldIdentity, validity: CacheValidity)
}
```

Sampled content carries its own truth contract:

```wrela
data SampledContent {
    provenance: ContentProvenance
    planes: Slice<SampledPlane>
    color_space: ColorSpace
    filter: SampleFilter
    trust: ContentTrust
    frame_id: MediaFrameId
    validity: CacheValidity
}
```

`ContentProvenance` names where sampled bytes came from: decoded image file,
video stream, camera frame, screenshot, PDF embedded image, remote surface, or
other declared source.

The sampled bytes themselves are authority-bearing, bounded memory views:

```wrela
data SampledPlane {
    buffer: MediaBufferView
    plane: U32
    width: U32
    height: U32
    stride_bytes: U32
    pixel_format: PixelFormat
    decode_epoch: U64
}

data MediaBufferView {
    authority: MediaBufferAuthority
    lifetime: BufferLifetime
    bytes: Slice<U8>
}
```

The renderer samples only through these bounded views. A sampled field must make
buffer ownership, lifetime, format, stride, decode epoch, and trust visible
before it can participate in the frame.

PDFs, SVGs, documents, maps, and other mixed media should project as much as
possible into analytic fields. Text and vector paths can become normal fields.
Embedded images, video planes, camera frames, screenshots, and remote pixels
remain sampled fields with explicit provenance and trust.

## Field Emission

Apps and desktop components do not draw pixels. They emit field scopes, fields,
input targets, and semantic nodes into a frame.

Representative frame shape:

```wrela
data FrameGraph {
    outputs: DisplayFrameInfoList
    root: ScopeIdentity
    scopes: FieldScopeList
    reports: FrameReportSink
}
```

Field scopes preserve temporal identity before rendering:

```wrela
data FieldScope {
    identity: ScopeIdentity
    parent: Option<ScopeIdentity>
    transform: Transform
    clip: Clip
    budget: FrameBudgetLease
    cadence: CadencePolicy
    dependencies: FieldDependencySet
    invalidation: InvalidationCause
    durable_watermark: Option<EventId>
    pending_ops: Slice<PendingOpId>
    fields: FieldList
    input_targets: InputTargetList
    semantic_nodes: SemanticNodeList
}
```

The renderer may flatten scopes into per-tile candidate lists, but the pre-bin
representation should keep hierarchy, stable identities, dependencies, and
invalidation causes. That lets the desktop know what changed, why it changed,
and which caches can be reused.

Every emitted field, input target, and semantic node belongs to exactly one
scope. The root scope owns shell/global fields such as the desktop background,
cursor, and overlays. There is no anonymous top-level field list; duplicate
membership would make rendering, cache invalidation, and provenance ambiguous.

Cadence is separate from budget:

```wrela
enum CadencePolicy {
    DisplayPaced
    OnDirty
    OnInputOnly
    FixedHz(hz: U32)
    MediaRate(frame_rate: Rational)
}
```

Budget answers "how much may this scope spend when it participates." Cadence
answers "when should this scope participate at all."

Representative component emission:

```wrela
fn emit_text_box(box: TextBox, scope: FieldScopeWriter, budget: FrameBudgetLease) {
    scope.push_field(budget, text_box_background(box))?
    scope.push_field(budget, text_box_border(box))?

    emit_text_line(
        line = box.line,
        transform = Transform2D.translate(box.inner.origin),
        clip = Clip2D.rect(box.inner),
        z_base = box.z + 20,
        scope = scope,
        budget = budget
    )

    if box.focused {
        scope.push_field(budget, text_box_caret(box))?
    }

    scope.push_input_target(budget, text_box_input_target(box))?
    scope.push_semantic_node(budget, text_box_semantics(box))?
}
```

Visual nesting remains component-owned and identity-bearing. Per-tile renderer
input remains flat:

```text
textbox background
textbox border
selection rect
glyph W
glyph r
glyph e
caret
```

The renderer does not need to understand "TextBox" to render a text box.

## Reference Renderer

The first renderer should be intentionally direct.

Representative frame loop:

```wrela
fn render_frame(frame: FrameGraph, surface: Surface) {
    let strategy = RendererStrategy.Avx2Packets(width = 8)
    let tiles = TileGrid(
        width = surface.width,
        height = surface.height,
        tile_width = 16,
        tile_height = 16
    )

    for scope in frame.scopes {
        if scope.budget.exhausted() {
            frame.reports.record_budget_miss(scope.identity)
        }

        for field in scope.fields {
            for tile in tiles.overlap(field.support) {
                tile.candidates.push(field)
            }
        }
    }

    for tile in tiles {
        tile.candidates.sort_by_z()

        for packet in tile.pixel_packets(strategy.width) {
            let p = packet.pixel_centers()
            var out = Color.transparent()

            for field in tile.candidates {
                if field.clip.reject_packet(packet) {
                    continue
                }

                let d = field.eval(field.data, p)
                let src = field.shade(field.data, d, p)
                out = over(dst = out, src = src)
            }

            surface.store(packet, out)
        }
    }
}
```

This is deliberately plain. The strategy is explicit, not guessed by a hidden
optimizer. Later renderers may add packet compaction, multi-threaded tile
scheduling, retained field IR deltas, layer caches, damage tracking, GPU
dispatch, or specialized solvers. They should still consume the same visible
field contracts and produce the same kind of cost report.

## Layered Surface UI

Layered 2D UI is a planar/surface-constrained case of the unified field model.
A normal text box is a stack of fields on a surface:

```text
z=10  desktop or window background
z=20  text box gray rounded rect
z=21  border or focus ring
z=30  selection highlight
z=40  glyph fields
z=50  caret field
z=60  cursor or overlay
```

The renderer sorts candidates by z within each tile and applies `over`
composition from back to front. Each field emits premultiplied color with
coverage.

Representative rounded text box background:

```wrela
pure lane fn text_box_background_eval(
    box: TextBoxData,
    p: Point2<Lane>
) -> F32<Lane> {
    return sdf_rounded_rect(
        p - splat(box.rect.center),
        splat(box.rect.half_size),
        splat(box.radius)
    )
}

pure lane fn text_box_background_shade(
    box: TextBoxData,
    d: F32<Lane>,
    p: Point2<Lane>
) -> Color<Lane> {
    return color(0.18, 0.19, 0.21, 1.0) * smooth_coverage(d)
}
```

The white text above it is just more fields with higher z.

## Text

Text is a core desktop subsystem, not a renderer detail. Text should be explicit
data plus glyph fields, caret stops, selection semantics, IME composition state,
accessibility metadata, and cache validity. The renderer should not hide a font
engine.

For a simple line:

```wrela
data GlyphInstance {
    cluster: RangeU32
    origin: Vec2
    advance: F32
    bounds: Rect
    sdf: pure lane fn(Point2<Lane>) -> F32<Lane>
}

data TextLine {
    text: Bytes
    glyphs: Slice<GlyphInstance>
    carets: Slice<F32>
    baseline: F32
    ascent: F32
    descent: F32
    selection: RangeU32
    caret: U32
}
```

A line of text emits:

- selection highlight fields behind glyphs
- one field per visible glyph
- one caret field when focused
- one input target for hit testing

Representative glyph wrapper:

```wrela
pure lane fn glyph_eval(glyph: GlyphInstance, p: Point2<Lane>) -> F32<Lane> {
    return glyph.sdf(p - splat(glyph.origin))
}

pure lane fn glyph_shade(
    glyph: GlyphInstance,
    d: F32<Lane>,
    p: Point2<Lane>
) -> Color<Lane> {
    return color(0.95, 0.97, 1.0, 1.0) * smooth_coverage(d)
}
```

The preferred long-term shape is generated literal SDF functions per glyph.
This keeps glyphs honest: text is not an opaque bitmap atlas as source truth.

Small/body text should use derived glyph coverage caches from the start. Such
caches are acceleration artifacts with validity, not authored source, but they
are part of the load path for a text-heavy desktop. The cache key should include
glyph identity, size, transform, color/subpixel policy, display scale, color
mode, and font/shaping version.

Real text layout remains a hard subsystem. Unicode clusters, shaping,
ligatures, bidi, fallback, color emoji, hinting policy, keyboard selection,
IME composition, and text alternatives should live in a visible Wrela
text-layout library that produces glyph instances, caret stops, input targets,
and semantic nodes.

## Selection And Caret Input

Selection uses the same data as rendering.

The text line keeps caret stops:

```text
carets[0] = before first cluster
carets[n] = after last cluster
```

Hit testing maps screen coordinates into text content coordinates and chooses
the nearest caret stop:

```wrela
fn text_line_hit_test(line: TextLine, p: Vec2) -> TextHit {
    var best = 0
    var best_dist = abs(p.x - line.carets[0])

    for i in 1..line.carets.length {
        let d = abs(p.x - line.carets[i])
        if d < best_dist {
            best = i
            best_dist = d
        }
    }

    return TextHit(caret = best)
}
```

Pointer down stores a selection anchor. Pointer drag updates the focus. The
next frame emits a selection field behind the glyphs.

There is no special selection renderer. Selection is a field.

## Scrolling

Scrolling is a coordinate transform plus clipping.

Representative scroll view:

```wrela
data ScrollView {
    rect: Rect
    content_size: Vec2
    scroll: Vec2
}
```

Child content lives in content coordinates. Emission maps content coordinates
to screen coordinates:

```text
screen = viewport_origin + content - scroll
```

The scroll view emits only visible children:

```wrela
fn emit_scroll_view(
    view: ScrollView,
    children: Slice<Item>,
    frame: FrameGraph,
    budget: FrameBudgetLease
) {
    let scope = frame.scope(view.scope)
    scope.push_field(budget, scroll_background(view))?

    for child in children {
        let screen_bounds = child.bounds
            .translate(view.rect.origin)
            .translate(-view.scroll)

        let visible = screen_bounds.intersect(view.rect)
        if visible.is_empty() {
            continue
        }

        child.emit(
            transform = Transform2D.translate(view.rect.origin - view.scroll),
            clip = Clip2D.rect(view.rect),
            visible = visible,
            frame = frame,
            budget = budget
        )
    }

    emit_scrollbar(view, frame)
}
```

Input reverses the same transform:

```wrela
fn scroll_view_hit_test(view: ScrollView, p_screen: Vec2) -> Hit {
    if !view.rect.contains(p_screen) {
        return Hit.none()
    }

    let p_content = p_screen - view.rect.origin + view.scroll
    return hit_test_children(view.children, p_content)
}
```

Scrolling does not require a special renderer or hidden view system.

## Input Targets

Rendering fields and input targets are emitted from the same state.

Representative input target:

```wrela
data InputTarget {
    identity: InputIdentity
    z: I32
    support: FieldSupport
    space: InputSpace
    capture: CapturePolicy
    data: InputData
    hit: fn(InputData, InputQuery) -> HitResult
    event: fn(InputData, InputEvent) -> AppAction
}
```

Representative input space:

```wrela
enum InputSpace {
    Surface(surface: SurfaceFrame)
    Desktop2D
    Ray3D(camera: CameraFrame)
}

enum InputQuery {
    Point2D(p: Vec2)
    SurfacePoint(surface: SurfaceFrame, p: Vec2)
    Ray(origin: Vec3, direction: Vec3)
}
```

Global hit testing walks candidates from top to bottom, using support regions
for cheap rejection and target hit functions for semantic detail.

Pointer capture is explicit state owned by the desktop core. If a drag starts
inside a text line, the text line can keep receiving pointer moves until release
even when the pointer leaves the original support rect.

Input coordinate transforms mirror visual transforms. If a scroll view moves
content by `-scroll`, its hit path maps input back by `+scroll`.

## Human Semantics And Accessibility

Fields are a strong substrate for accessibility because every visible thing
already has identity, support, z/order, source state, input target shape,
temporal identity, and provenance. Accessibility should be emitted from the same
source as fields and input targets, not bolted on as a parallel tree.

Representative semantic node:

```wrela
data SemanticNode {
    identity: SemanticIdentity
    field: Option<FieldIdentity>
    input: Option<InputIdentity>
    role: SemanticRole
    name: TextAlternative
    value: SemanticValue
    actions: SemanticActionList
    focus: FocusPolicy
    bounds: FieldSupport
    children: Slice<SemanticIdentity>
}
```

The desktop should model:

- focus order
- keyboard commands
- command routing
- labels and text alternatives
- roles, values, states, and actions
- screen reader traversal
- switch access and keyboard-only access
- magnification and high-contrast affordances
- undo/redo command semantics where the app exposes them

Semantic nodes and input targets may share identity, but they are not the same
thing. A visual object can be readable but not clickable; a gesture target can
have a semantic command; text can expose caret and selection semantics beyond
pixel hit testing.

## Unified 2D And 3D Fields

Wrela should keep normal work surfaces planar where that is better for
legibility. But the desktop itself should not be locked into flat rectangles.

The geometric model is unified 3D fields. Planar UI is a constrained case: a
field lives on a known surface, has a cheap projected support, and can use a
specialized 2D execution strategy. This keeps identity, provenance, cache
validity, input, accessibility, and reports unified while preserving the fast
2D path.

The field model supports 3D objects without meshes:

- app icons as small 3D field objects with click animations
- a spatial desktop background
- screensaver/default background as animated field geometry
- 3D affordances where depth is genuinely delightful or useful

Representative surface binding:

```wrela
data SurfaceFrame {
    origin: Vec3
    x_axis: Vec3
    y_axis: Vec3
    scale: Vec2
}
```

The surface normal is derived from `x_axis x y_axis`. Constructors should reject
degenerate or non-orthogonal frames rather than storing a normal that can drift
from the axes.

The solver ladder should prefer:

```text
projected analytic 2D solve
analytic 3D hit
support interval jump
bounded sphere tracing
bounded ray marching fallback
```

Ray marching should not become the baseline for desktop UI. Most UI fields
should be closed-form.

Hit testing uses the same field data. A 3D desktop icon can expose a ray-hit
input target that returns the object, surface point, normal, and interaction
state.

## Visual Style Constraints

The desktop should lean into the strengths of analytic fields:

- crisp glyphs
- precise panels
- clean rounded shapes
- explicit highlights
- subtle gradients
- sharp, readable edges
- soft analytical shadows where useful
- field-native 3D geometry for delight

It should avoid blur-heavy visual identity. Backdrop blur and liquid-glass blur
are not first-class needs. Soft shadows are acceptable because they communicate
depth and can be represented as bounded analytical fields or derived shadow
fields.

## Display Output

The first display milestone uses UEFI GOP because it is the simplest path to
put pixels on real hardware without a production GPU driver.

The likely first path:

```text
boot through UEFI
discover GOP framebuffer mode
allocate internal 1080p surface
render field desktop into internal surface
scale or copy into GOP framebuffer
present at measured/known cadence
```

Limitations:

- GOP mode selection may be limited.
- Hardware refresh may be 60Hz even if the renderer can produce more.
- Vblank/page-flip control may be unavailable or weak.
- Multi-display support depends on firmware exposure.
- Tearing may be possible until a real display driver exists.

Later display drivers can replace the physical scanout path without replacing
the field renderer.

AMDGPU or integrated AMD graphics are plausible later targets, but they should
not be required for the first beautiful desktop proof.

### Display Backend Ladder

Wrela should separate rendering from scanout. The field renderer produces an
internal surface from field records. A display backend owns how that surface
reaches an output and what timing facts are trustworthy for that output.

Representative backend responsibilities:

```wrela
data DisplayBackend {
    identity: DisplayBackendIdentity
    kind: DisplayBackendKind
    outputs: fn() -> Slice<DisplayOutput>
    capabilities: fn(DisplayIdentity) -> DisplayCapabilities
    map_surface: fn(DisplayIdentity, SurfaceDesc) -> Framebuffer
    present: fn(DisplayIdentity, Framebuffer, PresentPolicy) -> PresentResult
}
```

The backend should report capabilities instead of pretending every target is a
modern display driver:

```wrela
data DisplayCapabilities {
    modes: Slice<DisplayMode>
    has_vblank_event: Bool
    has_page_flip: Bool
    has_hw_cursor: Bool
    has_hotplug: Bool
    supports_multiple_outputs: Bool
    cadence_confidence: CadenceConfidence
}
```

The practical ladder is:

```text
1. Firmware framebuffer
   UEFI GOP-provided mode and framebuffer. This is the real-hardware boot path
   and universal fallback.

2. Deterministic virtual framebuffer
   No hardware dependency. Used for renderer tests, replay fixtures, pixel
   hashes, frame reports, simulated cadence, and CI.

3. QEMU Bochs or standard VGA display
   A simple virtual PCI display with a linear framebuffer and basic mode
   registers. Useful for testing PCI discovery, mode setup, framebuffer mapping,
   and non-GOP boot paths without vendor GPU complexity.

4. Virtio-gpu 2D
   The first modern virtual display-controller protocol target. Useful for
   testing queues, resource backing, transfers/flushes, multi-output shape, and
   a protocol that is closer to deployed virtual machines than raw VGA.

5. Native AMD and Intel display drivers
   Display-controller support first: enumerate outputs, read modes, allocate or
   map scanout buffers, flip pages, observe vblank/cadence, handle hotplug, and
   expose cursor planes where available. This is not the same milestone as a
   production 3D or compute stack.

6. GPU render acceleration
   A later explicit `GpuCompute` or GPU-render strategy for field execution.
   It should consume the same field contracts and report the same provenance,
   fallback, and budget facts as the CPU renderer.
```

This keeps the first desktop honest. GOP can prove the visible field world on
real machines. Virtual and QEMU backends can make display behavior testable.
Native AMD and Intel work can start with presentation truth before Wrela takes
on full GPU programming.

## Color And Output Truth

Color is part of the field contract. Shade functions should produce colors in a
declared working space, preferably scene-linear. Presentation owns tone mapping,
SDR/HDR conversion, display color-space conversion, pixel-format conversion,
scaling filters, and framebuffer memory policy.

Representative output policy:

```wrela
data OutputColorPolicy {
    working_space: ColorSpace
    output_space: ColorSpace
    transfer: TransferFunction
    hdr: HdrPolicy
    pixel_format: PixelFormat
    scale_filter: ScaleFilter
    subpixel_text: SubpixelTextPolicy
}
```

Frame reports should record color policy, output pixel format, scaling path,
tone-map path, and whether sampled content was converted, clipped, or
approximated.

## Debug Surface Is Product Surface

Inspect mode, frame reports, provenance, accessibility tree views, budget
reports, and replay are not developer-only leftovers. They are product surfaces
that prove Wrela owns the visible world. A user or developer should be able to
ask "why is this here?" and get a source-visible answer without attaching an
external debugger.

## Rate Matching

Internal design target and physical scanout are separate.

Default targets:

```text
design target:       1080p internal @ 120fps
minimum good mode:   1080p internal @ 60fps
fallback mode:       720p internal @ 120fps
showcase mode:       1440p internal @ 120fps if measured headroom exists
not first target:    4K internal @ 120fps
```

If physical scanout is 60Hz, Wrela should normally present at 60 frames per
second. Rendering 120 full frames for a 60Hz output wastes work and can create
worse pacing if frames are discarded unevenly.

The correct policy:

```text
input sampling:       as high as device support allows
app/world tick:       display-paced or explicitly fixed
render/present:       matched to physical scanout
animation time:       monotonic real time, not frame count
frame construction:   just in time before presentation
```

Representative clock:

```wrela
data FrameClock {
    input_hz: U32
    simulation_hz: U32
    presentation_hz: U32
    display_period_ns: U64
}
```

The frame report should record detected scanout cadence, chosen presentation
cadence, missed deadlines, and whether frame production is rate-limited by
hardware output.

Display clocks are per output. A laptop panel at 120Hz and an external monitor
at 60Hz should not force the whole desktop to the slowest output. The desktop
maintains shared retained field IR, then runs per-output render/present passes
at each output's cadence:

```text
shared world tick:
  update scopes that are dirty, input-driven, or cadence-due

per-output pass:
  select intersecting scopes
  render with that output's mode, scale, color policy, and presentation clock
  present at that output's cadence
```

A window spanning displays may be rendered into both output passes. Animation
time remains monotonic real time, not frame count.

## Latency And Late Latching

FPS is not enough. The desktop should report and optimize input-to-present
latency.

Initial targets:

```text
pointer sample-to-present: one display period when the frame is on time
keyboard input-to-visible edit: one display period for local text edits
cursor late-latch: after app tick, before render/present
```

Input should keep device histories where useful: high-Hz pointer samples,
coalesced pointer movement, keyboard repeat, touch/stylus history, and the frame
that consumed each sample. Cheap input-bound overlays may run in a late lane
after normal app tick:

```text
normal tick:
  app state, layout, durable completion polling

late lane:
  cursor
  hover highlight
  drag preview
  cheap scroll/gesture feedback
```

The late lane is a typed scope with a strict budget. It must not run arbitrary
app work or storage/network operations.

## Performance Model

1080p120 is approximately:

```text
1920 * 1080 * 120 = 248,832,000 pixels/sec
RGBA final write    = about 1 GB/sec
8.33 ms/frame       = frame budget
```

The memory bandwidth is not the hard part on modern hardware. The hard part is
field evaluation cost.

Cost is roughly:

```text
visible pixels * average candidate fields per pixel * average field cost
```

The core performance strategy is:

- keep supports cheap
- cull by tile before evaluation
- use analytic solves when possible
- keep field evals pure and vectorizable
- avoid hidden allocation in the hot path
- avoid per-pixel dynamic dispatch
- keep tile data resident
- report the cause of every expensive frame

The no-cache path should stay readable and correct, but damage tracking,
retained field deltas, and glyph coverage caches are part of the desktop power
story. Full redraw is a useful proof path; it should not be the steady-state
idle desktop path.

## Backend-Neutral Field Execution

The renderer should use packets, lanes, or waves as its natural execution unit,
but field source should not bake in a CPU vector width. Wrela should use a
single-program, multiple-data style: write the field function once over a
logical sample, then run it over the explicit execution strategy selected by the
image.

Representative strategy shape:

```wrela
data FieldRenderer {
    eval_strategy: EvalStrategy
    fallback: EvalStrategy
    allow_gpu: Bool
}

enum EvalStrategy {
    Scalar
    Avx2Packets(width: U32)
    Avx512Packets(width: U32)
    GpuCompute(device: GraphicsDevice, lane_group: U32, max_dispatch_us: U64)
}
```

The first renderer can be AVX2-first with scalar fallback. AVX-512 is a
preferred later CPU path when hardware and interrupt-save policy allow it. GPU
or accelerator paths should be explicit extensions of the same field contract,
not a translation layer over source that was shaped around a fixed CPU packet
type.

Lane functions may diverge per lane. Conditional branches and loops in `pure
lane fn` bodies lower through an explicit mask discipline chosen by the backend.
Purity, boundedness, and frame-safety still apply. The exact language rules for
`Lane` deserve their own sub-design; this desktop design depends only on the
contract that lane divergence is explicit and reportable, not hidden scheduler
work.

If Wrela emits AVX instructions in interruptible frame code, the platform's
interrupt save/restore policy must preserve the relevant vector state. The
compiler should reject an image that combines vectorized interruptible frame
execution with an unsafe interrupt-save policy.

Compiler responsibility should stay narrow:

- enforce `pure` for field eval and shade functions
- reject frame-unsafe operations inside pure fields
- lower lane-abstract operations to the selected target
- report unsupported target features
- preserve field contracts in reports
- report the selected strategy, lane width, and fallback use

The compiler should help specialize visible field execution without becoming a
hidden graphics optimizer. Call-site specialization, structure-of-arrays layout
for hot field data, and support-algebra derivation are good when they are
reported and reproducible.

## Temporal Identity And Caching Strategy

The first renderer should keep a readable direct field path, but desktop-scale
performance depends on temporal identity and explicit cache validity. A desktop
where 99% of visible fields are unchanged should not rebuild and re-evaluate
the whole world every frame.

Fields, scopes, sampled content, glyph runs, and caches should carry stable
identity, dependencies, and invalidation causes:

```wrela
data TemporalIdentity {
    stable_id: FieldIdentity
    version: U64
    dependencies: FieldDependencySet
    invalidation: InvalidationCause
}
```

The source of truth remains the field world. The frame representation may be a
retained field IR with delta mutation. Renderer input may still flatten into
per-tile candidates.

Clear cache slots:

- tile candidate cache
- layer/surface cache
- glyph coverage cache
- shadow cache
- field support cache
- transformed support cache
- display scale/blit cache

Rules:

- caches are derived artifacts
- caches carry source identity, transform, style, scale, clip, and validity
- glyph coverage caches are load-bearing for body text
- damage tracking and retained field deltas are load-bearing for power
- cache use is visible in frame reports
- cache misses cannot change visual truth

This keeps the model honest. Caches make the field world faster; they do not
replace it.

## Realtime Desktop Core

The primary desktop model is a synchronous frame world.

Representative loop:

```wrela
loop {
    let input = input_core.sample()
    let dt = desktop_clock.next_world_dt()

    let frame = desktop.begin_frame(dt, input)

    desktop_shell.tick(dt, input, frame.scope(frame.root))

    for window in desktop.visible_windows_due(input, dt) {
        let scope = frame.scope(window)
        if window.focused || window.dirty || window.animating {
            window.app.tick(dt, input.for_window(window), scope)
        } else {
            window.app.emit_static(scope)
        }
    }

    desktop_shell.run_late_lane(input, frame)

    for output in desktop.outputs_due(dt) {
        renderer.render_output(frame, output, output.internal_surface)
        output.present(output.internal_surface)
    }

    reports.publish(frame.costs)
}
```

This is intentionally game-engine-shaped.

On multi-core machines, visible app ticks may run in parallel when each app
writes to disjoint scopes and consumes its own budget capability. The desktop
barrier-merges scopes before tile binning, preserving deterministic z-order,
focus, input capture, and report ownership.

The desktop owns:

- display timing
- global frame assembly
- focus
- z-order
- window placement
- visibility and occlusion
- pointer capture
- keyboard routing
- cursor
- shell overlays
- presentation
- frame reports

Apps own:

- app state
- frame-bounded visible logic
- field emission
- input target emission
- text/layout internals
- app-specific semantic hit behavior

## Visibility And Participation

Visibility decides whether app work is needed.

Suggested states:

```text
hidden:
  no visual participation; background jobs only if allowed

fully occluded:
  visual snapshot may be retained; no field rendering needed

visible clean:
  may emit static fields or reuse stable emission data

visible dirty:
  participates in the frame

focused:
  receives input and highest foreground priority
```

The strict owned-app model says: if visible/focused code misses the frame
budget, fix that code. Make parsing incremental. Bound layout. Cache glyph
runs. Split unbounded work onto a job lane.

Budget should be mechanical, not just cultural. Visible apps receive a consumed
`FrameBudget` or `FrameLease` capability that limits how much foreground work
they may spend in the current frame:

```wrela
data FrameBudget {
    max_tick_ns: U64
    max_emit_ns: U64
    max_fields: U32
    max_unknown_supports: U32
    max_sampled_bytes_touched: U64
}

data FrameBudgetLease {
    policy: FrameBudget
    consumed: FrameBudgetCounters
}
```

An app can choose to spend its budget on tick, layout, field emission, semantic
emission, or late input work. Countable resources must fail mechanically when
exhausted:

```wrela
scope.push_field(budget, field) -> Result<(), BudgetExhausted>
scope.push_input_target(budget, target) -> Result<(), BudgetExhausted>
scope.push_semantic_node(budget, node) -> Result<(), BudgetExhausted>
budget.charge_unknown_support() -> Result<(), BudgetExhausted>
budget.charge_sampled_bytes(count) -> Result<(), BudgetExhausted>
```

Time budgets should be measured and reported, and where the runtime can enforce
them before a merge point it should refuse further emission from the exhausted
scope. Unknown supports, unbounded field counts, and expensive sampled content
must be capped for foreground participation. The goal is not a hidden scheduler
rescue; it is a source-visible authority for frame time.

Snapshot boundaries still exist, but they are not the default foreground model.

They are for:

- hidden/background work
- untrusted or legacy apps
- app failure recovery
- possibly expensive app surfaces that are explicitly not realtime

## Scheduler Lanes

The whole machine should have distinct workload lanes.

Conceptual lanes:

```text
realtime frame core:
  input sampling
  visible app tick
  layout for visible regions
  field emission
  tile binning
  rendering
  presentation

foreground storage core:
  StorageWriter
  event ID assignment
  group commit
  foreground NVMe queue
  durable frontier
  durability completions

maintenance storage core:
  projection maintenance
  page/blob cache
  blob relocation
  orphan scanning
  checkpoint rebuild

network reactor core:
  NIC rings
  packet processing
  protocol timers
  TLS/QUIC/HTTP work as budgeted

job lanes:
  parsing
  indexing
  thumbnail decode
  compression
  compilation
  AI calls
```

On machines with enough cores, the realtime core should be pinned away from
storage and network work. On smaller machines, explicit placement/runtime policy
should still treat frame work as deadline work and storage/network/jobs as
preemptible.

The hop between lanes is intentional. A bounded queue hop is cheaper than a
missed frame caused by blocking storage, lock contention, page faults, or
unbounded parsing.

Application logic remains foreground-owned. The app decides what a user action
means, what semantic events to request, and what pending UI state to show. The
foreground storage core owns durable truth mechanics: event IDs, batching,
NVMe write/flush/FUA sequencing, durable-frontier advancement, and durability
completion publication.

This split keeps the frame rule simple: the realtime frame core never waits for
durability. It submits semantic storage requests and polls durable facts.

## Async Programming Model

The first desktop async model is explicit submit plus explicit completion.
`submit` is non-blocking. It either places a request into a bounded queue and
returns a ticket, or it returns immediate backpressure/failure.

Representative storage request shape:

```wrela
match storage.try_append(op_id = op.id, group = events) {
    AppendAccepted(ticket) => editor.pending.push(ticket)
    AppendBackpressure => editor.show_saving_delayed()
    AppendRejected(reason) => editor.mark_storage_error(reason)
}
```

The storage core does the actual waiting. Its loop follows Wrela's existing
executor pattern:

```text
drain ready work
arm wait sources
recheck ready work
sleep until interrupt/topic/wake
```

The frame core only polls:

```wrela
while let Some(done) = storage.durable.try_take(max_count = 4) {
    editor.apply_durability_fact(done)
}
```

Future `await` syntax may be useful in non-realtime storage, network, or job
executors, but only as sugar for visible wait sources. It should not be legal
inside frame tick, render, field eval, shade, layout hot paths, or any pure
field function.

This should be enforced as a typed effect, not a documentation promise:

```wrela
effect frame_safe
effect may_wait

frame_safe fn editor_tick(...)
pure lane frame_safe fn glyph_eval(...)
may_wait fn storage_worker_loop(...)
```

`frame_safe` code may submit bounded async requests and poll bounded completion
streams. It may not wait, await, block on storage/network, perform unbounded
allocation, or call a `may_wait` function. Library authors should not be able to
smuggle a future await into frame code.

## Storage Boundary

Storage is async relative to the frame.

The frame truth equation is:

```text
Frame =
  durable/app state
  + input samples
  + async completion facts
  + time
  + declared renderer strategy
```

Every subsystem should be able to say which terms it reads, mutates, records, or
reports.

Frame participants may:

- submit storage work without waiting
- poll storage completion streams
- apply completed results within a budget
- render pending/stale/error states

Frame participants may not:

- await storage durability
- block on file reads
- block on file writes
- perform unbounded filesystem traversal
- page fault unpredictably in field eval
- read storage inside pure field functions

Representative shape:

```wrela
fn editor_tick(editor: Editor, input: InputFrame, frame: FrameGraph) {
    editor.apply_input_now(input)

    while let Some(done) = editor.storage.durable.try_take(max_count = 4) {
        editor.apply_durability_fact(done)
    }

    editor.submit_new_storage_requests()
    editor.layout_visible_region()
    editor.emit_fields(frame)
}
```

Network follows the same rule. The UI side of a network app is realtime. The
socket/download/protocol side is async.

### Field IR As Projection

Retained field IR is the final visible projection of app and durable state.
Pending UI is a not-yet-durable projection overlay.

```text
event log
  -> durable projection
  -> app visible state
  -> retained field IR
  -> tile candidates
  -> pixels
```

A visible scope should carry the durable watermark and pending operation set it
reflects. That makes saved, saving, unsaved, stale, and conflicted state a
property of projection watermarks instead of a separate hand-maintained UI flag:

```text
saved:
  scope depends only on durable events

saving:
  scope includes pending local events not durable yet

stale:
  scope is behind a known durable/projection watermark

error/conflict:
  pending projection was rejected or conflicted
```

The same `TemporalIdentity` and `InvalidationCause` machinery should serve
storage projections and visible field scopes. A `FileRenamed` event can
invalidate a directory projection row, the visible field scope for that row, and
the glyph fields for the old name while leaving unrelated rows alone.

## Durability Boundary

Durability has explicit states. A submitted operation is not durable just
because it entered a queue.

```text
accepted:
  request entered the bounded storage queue

durable:
  StorageWriter advanced the durable frontier after the required NVMe
  durability sequence

projection visible:
  derived read model caught up to the relevant event watermark
```

The storage writer may batch many app requests into one atomic group. Durability
completion can therefore be a ticket, event range, or watermark rather than a
per-event answer.

Representative app state:

```text
buffer_version = 42
last_submitted_version = 42
last_durable_version = 39
```

Each frame, the app may poll durability facts and update its visible state:

```text
saved:
  durable_version == buffer_version

saving:
  changes submitted but not durable

unsaved:
  local changes not yet submitted

error:
  storage rejected or failed a requested operation
```

Only storage-core completions can make durability claims. The app/frame side can
show immediate local truth and pending belief, but it must not pretend storage
work is complete before the storage writer says so.

## Business Logic Placement

Business logic splits by boundedness.

Frame-side logic:

- keystroke changes
- selection
- caret movement
- button pressed state
- small validation
- local command routing
- semantic event construction
- visible layout updates
- animation state
- field emission

Async/job-side logic:

- storage durability mechanics
- network
- database-like queries
- full-document parse
- huge syntax highlight passes
- search indexing
- thumbnail decode
- compile/build
- AI calls
- large import/export

Rule:

```text
If logic directly determines the next visible frame and is bounded, it may run
on the frame lane.

If it can block, allocate unpredictably, touch storage/network, or scale with
unbounded data, it must run outside the frame lane and return by message.
```

The storage core owns persistence mechanics, not application meaning. The app
still owns the semantic logic that decides which events to request and how
success, failure, stale data, or pending durability should appear.

## Optimistic UI

The frame lane never waits. Across async boundaries, the UI either owns the
truth immediately or displays an explicit pending belief.

Operation classes:

```text
local truth:
  apply immediately, no rollback
  examples: typing, caret movement, selection, local document edits

optimistic external mutation:
  apply immediately with pending status, then commit/rollback/reconcile
  examples: file rename, move, delete in a visible directory

confirmed external mutation:
  show pending/progress until confirmed
  examples: payment, privilege changes, destructive remote operation
```

Representative pending operation:

```wrela
data PendingOp {
    id: OpId
    status: PendingStatus
    apply: fn(AppState)
    rollback: fn(AppState)
    external: ExternalRequest
}
```

Failure does not always mean rollback. If a text buffer autosave fails, the
buffer remains edited and the app shows unsaved/error state. If a filesystem
rename fails, the directory view may roll back or show a conflict state.

Pending, failed, stale, unsaved, and committed states are visible app state.
They emit fields like anything else.

This is similar to awaiting a database in a traditional backend flow, except
the foreground frame does not suspend. The app records a pending request, keeps
rendering, and future frames observe completion facts.

## Multi-Display

The desktop should model display outputs explicitly even if the first hardware
path exposes only one.

Representative shape:

```wrela
data DisplayOutput {
    identity: DisplayIdentity
    physical_rect: Rect
    mode: DisplayMode
    clock: FrameClock
    scale: F32
    color: OutputColorPolicy
    framebuffer: Framebuffer
}

data DesktopSpace {
    outputs: Slice<DisplayOutput>
    virtual_bounds: Rect
}
```

Fields live in desktop space. Each display output renders the subset of scopes
and fields that intersects its physical/virtual region using that output's
clock, scale, color policy, and framebuffer. Multi-display rendering should use
shared retained field IR with per-output render passes, not a global lock to the
slowest output.

The first milestone may support:

```text
one output guaranteed
multiple GOP outputs opportunistic
virtual multi-output fixtures for renderer and frame-report tests
later virtio-gpu or native driver output enumeration
```

Multi-display support should not require app authors to know which monitor
they are on unless they explicitly ask.

## Cost Reports

Frame reports are part of the design, not debug leftovers.

A frame report should be able to answer:

- what display cadence was targeted?
- did the frame miss its deadline?
- which lane consumed the time?
- how many fields were emitted?
- how many supports were unknown?
- how many tiles were touched?
- average and max candidates per tile
- field eval count
- glyph eval count
- field execution strategy and lane width used
- cache hits and misses
- invalidation causes
- sampled field count and sampled bytes touched
- support rejections
- clip rejections
- budget consumption by scope
- layer cache use
- shadow cache use
- scaling/blit cost
- color conversion/tone-map cost
- present cost
- app tick costs by visible app
- storage/network/job completions consumed this frame
- semantic nodes emitted
- accessibility tree update cost
- input-to-present latency estimate
- pixel provenance availability

Representative report shape:

```text
Frame 18402:
  target: 120Hz / 8.33ms
  actual: 9.41ms missed
  input: 0.04ms
  app.text_editor.tick: 2.10ms
  app.text_editor.syntax_visible: 3.80ms
  field_emit: 0.44ms
  tile_bin: 0.61ms
  shade_tiles: 2.24ms
  scale_present: 0.18ms
  fields: 412
  tiles: 8160
  avg_candidates_per_touched_tile: 3.2
  max_candidates_per_tile: 19
  glyph_fields: 337
  field_eval_strategy: avx2_packets
  lane_width: 8
  sampled_fields: 2
  semantic_nodes: 41
  missed_reason: app.text_editor.syntax_visible
```

The report turns frame drops into fixable facts.

## Pixel Provenance And Replay

If Wrela owns the visible world, every pixel should be explainable. In inspect
mode, a pixel or field should be able to answer:

- which field produced this color?
- which app or shell component emitted the field?
- which source state, input sample, and completion watermark affected it?
- which sampled content or analytic function was used?
- which cache hit or miss happened?
- which input target and semantic node overlap it?
- what did it cost?

Representative provenance shape:

```wrela
data PixelProvenance {
    frame_id: FrameId
    field: FieldIdentity
    scope: ScopeIdentity
    app: AppIdentity
    input_sample: InputSampleId
    storage_watermark: Option<EventId>
    network_watermark: Option<NetworkEventId>
    cache: CacheProvenance
    cost: CostSlice
}
```

The same facts should support deterministic replay. A frame is a function of
durable/app state, sampled input, async completion facts, and time. Recording
those inputs should allow Wrela to replay a frame, reproduce a report, and use
frame reports as test fixtures for visual and performance regressions.

Per-field provenance should be always-on because it is bounded by emitted field
count. Per-pixel provenance should be inspector-only: when the user inspects a
pixel or tile, Wrela can rerun that tile with provenance tracking enabled and
reconstruct the contributing fields, caches, sampled content, semantic nodes,
and cost slices. It should not store a full provenance record per pixel for
every frame.

## Failure And Recovery

The primary model treats missed foreground deadlines as bugs. That does not
mean the system has no recovery path.

Possible recovery behavior:

- continue drawing cursor and shell overlays
- mark a visible app as late in diagnostics
- temporarily use the app's last stable field emission
- demote an app to snapshot mode after repeated deadline misses
- allow the user to close or restart a failing app
- report exact cost ownership

The degradation ladder should be deterministic and visible:

```text
1. consume fewer async completions this frame
2. reduce optional effects such as expensive shadows
3. lower app cadence where the app declared lower-rate tolerance
4. reuse last stable emission for the slow app
5. preserve cursor and shell overlays
6. demote repeated offenders to snapshot mode
```

This is an escape hatch, not the normal foreground contract. It should not hide
the original cause of a missed deadline.

## Security And Isolation

This design assumes trusted Wrela-owned foreground apps for the first desktop.
It does not claim hostile app isolation.

Still, the existing Wrela authority model should apply:

- apps should not forge framebuffer authority
- apps should not forge input authority
- apps should not forge storage/network authority
- apps should not touch other apps' state except through declared APIs
- app frame scopes should narrow where fields and input targets may be emitted
- compiler reports should show display, input, and async authorities

Later process isolation, page permissions, IOMMU policy, app signing, and
third-party app containment should extend this model.

## Milestone Shape

The first coherent desktop proof should prove the smallest end-to-end version:

```text
1. display backend boundary with GOP framebuffer output
2. internal 1080p surface
3. display-paced frame clock with rate matching
4. unified field records with support, semantics, eval, shade, and temporal identity
5. lane-abstract field eval with explicit AVX2 renderer strategy first
6. tiled CPU renderer with scalar fallback and reportable strategy selection
7. layered planar UI as a specialized surface-field path
8. text box, glyph fields, glyph coverage cache, caret, and selection
9. scroll view transform and clipping
10. sampled field boundary for at least one image or screenshot
11. input targets, pointer capture, and minimal semantic/accessibility nodes
12. enforced field-count/support/sample-byte budgets for visible app participation
13. cadence policies for visible scopes
14. global desktop frame loop
15. explicit submit/completion storage boundary for visible app state
16. separate foreground storage core for `StorageWriter` and durability
17. frame cost report with cache, budget, strategy, latency, and provenance fields
18. deterministic virtual framebuffer backend for CI, pixel checks, and replay
```

The richer Wrela Workbench proof should build on that:

```text
1. notebook/editor shell
2. visible saved/saving/unsaved durability frontier
3. field IR as projection watermarks for visible scopes
4. inspect mode for pixel, field, input, semantic, budget, and storage lineage
5. IME placeholder and broader text semantics
6. sampled media with explicit buffer authority
7. per-output render passes when more than one display exists
8. deterministic frame replay fixture for at least one captured interaction
9. QEMU Bochs/standard VGA or virtio-gpu 2D backend as the first non-GOP
   display protocol target
```

The first native hardware display proof should be deliberately narrower than a
GPU renderer:

```text
1. one AMD integrated-graphics machine and one Intel integrated-graphics machine
2. output enumeration and preferred mode selection
3. scanout buffer allocation or mapping
4. page flip or best available present operation
5. measured cadence/vblank confidence in frame reports
6. fallback to GOP or virtual framebuffer when native bring-up fails
```

The first visual demo should show:

- a field-rendered desktop background
- at least one beautiful text box
- white field-rendered text on a gray rounded field panel
- cursor movement
- text selection
- scrolling
- a simple 3D field desktop object or background element
- one sampled field with provenance
- basic accessibility/semantic inspection for visible controls
- visible saved/saving/unsaved durability state
- a frame report that explains the cost of the scene

## Open Questions

- How much text shaping should be implemented before the first demo versus
  stubbed with generated ASCII glyph SDFs?
- Should the first renderer use 16x16 tiles, 32x8 tiles, or a measured choice
  based on vector packet shape and cache behavior?
- How should field function pointers or method references lower in Wrela's
  early compiler without adding dynamic dispatch?
- How much of the semantic/accessibility node surface should be stable in the
  first demo?
- What is the first sampled field source: screenshot, decoded image, or camera
  frame fixture?
- What minimum provenance record is cheap enough to keep always-on?
- What exact `frame_safe` effect rules should the language enforce before the
  first desktop demo?
- What is the smallest useful `Lane` sub-design for per-lane divergence and
  mask lowering?
- What is the first 3D field object that is delightful enough to justify the
  cost without distracting from text clarity?
- What minimum frame report format should become stable enough for tests?
- How should repeated foreground deadline misses be surfaced to the user during
  early development?
- What is the smallest stable `DisplayBackend` API that can cover GOP,
  deterministic virtual output, QEMU display devices, virtio-gpu 2D, and later
  native AMD/Intel display paths?
- Should the first non-GOP protocol target be QEMU Bochs/standard VGA for
  simplicity, or virtio-gpu 2D for a more modern virtual display shape?
- Which specific AMD and Intel integrated-graphics machines should become the
  first hardware-in-loop validation targets?
- How should Wrela report cadence truth when a backend can measure elapsed
  present time but cannot receive a reliable vblank event?

## Summary

Wrela's desktop should be a single realtime field world.

The model is:

```text
sample input
tick visible bounded app logic
poll completion streams without waiting
submit async requests without waiting
emit fields, input targets, and semantic nodes
bin fields into tiles
execute lane-abstract fields with explicit strategies
compose layers
present at hardware cadence
report every cost
```

This preserves the philosophical core:

- no hidden renderer
- no ambient GUI runtime
- no blocking frame lane
- no meshes as visual truth
- no sampled media without provenance
- no mystery frame drops

The machine should feel awake because the whole foreground stack is organized
around the next visible frame.
