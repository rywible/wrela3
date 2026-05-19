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
- The older Wrela field-engine work explored semantic fields, support pruning,
  solver portfolios, tile candidates, shared acceleration artifacts, and
  detailed presentation cost reports.

This desktop design extends those ideas into the foreground user experience.
It treats the desktop as one realtime visible world that emits fields and input
targets each frame.

## Assumptions

- The first display path uses UEFI GOP-provided framebuffer modes. A production
  GPU driver is a later milestone.
- The first guaranteed display target is one monitor. Multiple displays are
  supported opportunistically if firmware exposes multiple GOP handles or later
  display drivers make them available.
- The design target is 1080p internal rendering at 120 presented frames per
  second when hardware scanout supports it.
- If hardware scanout is lower than the renderer's preferred rate, presentation
  matches hardware cadence instead of producing undisplayable frames.
- The renderer is CPU-first and vector-first: AVX-512 is the preferred path,
  AVX2 is the supported fallback.
- Every authored visual source is a field. Meshes, user-authored textures, and
  prebaked SDF assets are not source truth for the desktop model.
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

## Non-Goals

This design does not add:

- a POSIX window server
- an HTML/CSS/DOM-style layout engine
- a retained-mode scene graph hidden from Wrela source
- a GPU dependency for the first desktop milestone
- a production AMDGPU, Intel, or NVIDIA driver
- general-purpose process isolation for mutually distrusting desktop apps
- arbitrary third-party GUI app compatibility
- user-authored meshes as the desktop visual primitive
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
- 3D desktop objects
- screensaver geometry

Derived artifacts may accelerate rendering, but they are not the truth. If a
cache, tile table, or layer buffer disagrees with the field source, the field
source wins.

### The Renderer Is Visible

Wrela should not hide "the renderer" behind declarative UI machinery.

The desktop should provide rendering primitives and one readable reference
field renderer. The renderer is an ordinary low-level program over Wrela data
structures: fields, supports, tile lists, vector packets, clips, blends,
surfaces, and reports.

The source should make these decisions visible:

- how fields are collected
- how supports are computed
- how fields are binned into tiles
- how tiles are evaluated
- how z-order is applied
- how alpha/coverage compositing works
- which caches are used or rejected
- when presentation matches scanout cadence

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
dedicated async lanes and return results through queues, futures, messages, or
snapshots.

The frame lane may enqueue work and poll completed work. It must not wait for
it.

## Core Data Model

The renderer consumes explicit field records. The exact syntax will evolve with
the language, but the shape should remain simple.

Representative 2D field shape:

```wrela
data Field2D {
    identity: FieldIdentity
    z: I32
    support: Rect
    semantics: DistanceSemantics
    clip: Clip2D
    cache: CachePolicy
    data: FieldData
    eval: pure fn(FieldData, Vec2x8) -> F32x8
    shade: pure fn(FieldData, F32x8, Vec2x8) -> Colorx8
}
```

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
enum Support2D {
    Empty
    Rect(rect: Rect)
    RoundedRect(rect: Rect, radius: F32)
    Circle(center: Vec2, radius: F32)
    PeriodicGrid(bounds: Rect, cell: Vec2)
    Unknown
}
```

`Unknown` is allowed but expensive. It should be visible in reports and should
not be common in foreground UI.

## Field Emission

Apps and desktop components do not draw pixels. They emit fields and input
targets into a frame.

Representative frame shape:

```wrela
data FrameGraph {
    display: DisplayFrameInfo
    fields_2d: FieldList2D
    fields_3d: FieldList3D
    input_targets: InputTargetList
    reports: FrameReportSink
}
```

Representative component emission:

```wrela
fn emit_text_box(box: TextBox, frame: FrameGraph) {
    frame.fields_2d.push(text_box_background(box))
    frame.fields_2d.push(text_box_border(box))

    emit_text_line(
        line = box.line,
        transform = Transform2D.translate(box.inner.origin),
        clip = Clip2D.rect(box.inner),
        z_base = box.z + 20,
        frame = frame
    )

    if box.focused {
        frame.fields_2d.push(text_box_caret(box))
    }

    frame.input_targets.push(text_box_input_target(box))
}
```

Visual nesting remains component-owned. Renderer input remains flat:

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
    let tiles = TileGrid(
        width = surface.width,
        height = surface.height,
        tile_width = 16,
        tile_height = 16
    )

    for field in frame.fields_2d {
        for tile in tiles.overlap(field.support) {
            tile.candidates.push(field)
        }
    }

    for tile in tiles {
        tile.candidates.sort_by_z()

        for packet in tile.pixel_packets_8wide() {
            let p = packet.pixel_centers()
            var out = Colorx8.transparent()

            for field in tile.candidates {
                if field.clip.reject_packet(packet) {
                    continue
                }

                let d = field.eval(field.data, p)
                let src = field.shade(field.data, d, p)
                out = over(out, src)
            }

            surface.store(packet, out)
        }
    }
}
```

This is deliberately plain. Later renderers may add packet compaction,
multi-threaded tile scheduling, layer caches, damage tracking, or specialized
solvers. They should still consume the same visible field contracts and produce
the same kind of cost report.

## Layered 2D

Layered 2D is ordered field composition. A normal text box is a stack of
fields:

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
composition. Each field emits premultiplied color with coverage.

Representative rounded text box background:

```wrela
pure fn text_box_background_eval(box: TextBoxData, p: Vec2x8) -> F32x8 {
    return sdf_rounded_rect(
        p - splat(box.rect.center),
        splat(box.rect.half_size),
        splat(box.radius)
    )
}

pure fn text_box_background_shade(
    box: TextBoxData,
    d: F32x8,
    p: Vec2x8
) -> Colorx8 {
    return color(0.18, 0.19, 0.21, 1.0) * smooth_coverage(d)
}
```

The white text above it is just more fields with higher z.

## Text

Text should be explicit data plus literal glyph fields. The renderer should not
hide a font engine.

For a simple line:

```wrela
data GlyphInstance {
    cluster: RangeU32
    origin: Vec2
    advance: F32
    bounds: Rect
    sdf: pure fn(Vec2x8) -> F32x8
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
pure fn glyph_eval(glyph: GlyphInstance, p: Vec2x8) -> F32x8 {
    return glyph.sdf(p - splat(glyph.origin))
}

pure fn glyph_shade(glyph: GlyphInstance, d: F32x8, p: Vec2x8) -> Colorx8 {
    return color(0.95, 0.97, 1.0, 1.0) * smooth_coverage(d)
}
```

The preferred long-term shape is generated literal SDF functions per glyph.
This keeps glyphs honest: text is not an opaque bitmap atlas as source truth.

Small/body text may later use derived glyph coverage caches when measured. Such
caches are acceleration artifacts with validity, not authored source.

Real text layout remains a hard subsystem. Unicode clusters, shaping,
ligatures, bidi, fallback, and IME composition should live in a visible Wrela
text-layout library that produces glyph instances, caret stops, and input
targets.

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
fn emit_scroll_view(view: ScrollView, children: Slice<Item>, frame: FrameGraph) {
    frame.fields_2d.push(scroll_background(view))

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
            frame = frame
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
    support: Rect
    capture: CapturePolicy
    data: InputData
    hit: fn(InputData, Vec2) -> HitResult
    event: fn(InputData, InputEvent) -> AppAction
}
```

Global hit testing walks candidates from top to bottom, using support regions
for cheap rejection and target hit functions for semantic detail.

Pointer capture is explicit state owned by the desktop core. If a drag starts
inside a text line, the text line can keep receiving pointer moves until release
even when the pointer leaves the original support rect.

Input coordinate transforms mirror visual transforms. If a scroll view moves
content by `-scroll`, its hit path maps input back by `+scroll`.

## 3D Desktop Fields

Wrela should keep normal work surfaces 2D where that is better for legibility.
But the desktop itself should not be locked into flat rectangles.

The field model supports 3D objects without meshes:

- app icons as small 3D field objects with click animations
- a spatial desktop background
- screensaver/default background as animated field geometry
- 3D affordances where depth is genuinely delightful or useful

Representative 3D field:

```wrela
data Field3D {
    identity: FieldIdentity
    z: I32
    support: Bounds3D
    semantics: DistanceSemantics
    solver: Field3DSolver
    data: FieldData
    eval: pure fn(FieldData, Vec3x8) -> F32x8
    shade: pure fn(FieldData, SurfaceHitx8) -> Colorx8
}
```

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

Full redraw should be plausible for bounded UI when tile candidate lists stay
small. Damage tracking and layer caches may come later, but the no-cache path
should be pushed as far as practical first.

## Vector CPU Rendering

The renderer should use vector packets as its natural execution unit.

The source-level shape can remain ordinary Wrela code, but runtime/library
types should expose packet widths:

```wrela
Vec2x8
Vec2x16
F32x8
F32x16
Colorx8
Colorx16
```

AVX-512 is the preferred path for modern high-end machines. AVX2 is the
fallback tier. Scalar fallback can exist for bring-up and tests, but it should
not define the performance target.

Compiler responsibility should stay narrow:

- enforce `pure` for field eval and shade functions
- reject frame-unsafe operations inside pure fields
- lower vector operations to the selected target
- report unsupported target features
- preserve field contracts in reports

The compiler should not become a hidden graphics optimizer.

## Caching Strategy

The first renderer should prove the direct field path before leaning on caches.
But the design should reserve clear cache slots:

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
- caches are optional
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
    let dt = display_clock.next_dt()

    let frame = desktop.begin_frame(dt, input)

    desktop_shell.tick(dt, input, frame)

    for window in desktop.visible_windows() {
        if window.focused || window.dirty || window.animating {
            window.app.tick(dt, input.for_window(window), frame.scope(window))
        } else {
            window.app.emit_static(frame.scope(window))
        }
    }

    desktop_shell.emit_cursor(input.pointer, frame)

    renderer.render(frame, internal_surface)
    display.present(internal_surface)
    reports.publish(frame.costs)
}
```

This is intentionally game-engine-shaped.

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
realtime frame lane:
  input sampling
  visible app tick
  layout for visible regions
  field emission
  tile binning
  rendering
  presentation

storage lane:
  NVMe queues
  filesystem/event-store work
  page/blob cache
  writeback
  projection maintenance

network lane:
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

On machines with enough cores, the realtime lane should be pinned away from
storage and network work. On smaller machines, the scheduler should still treat
frame work as deadline work and storage/network/jobs as preemptible.

The hop between lanes is intentional. A bounded queue hop is cheaper than a
missed frame caused by blocking storage, lock contention, page faults, or
unbounded parsing.

This does not require moving durable truth ownership away from the foreground
side of the application. The storage design can still keep a single
`StorageWriter` authority for accepting semantic events. The frame lane must
not perform the uncertain physical work behind that authority: NVMe command
completion, writeback, projection maintenance, blob maintenance, and similar
operations remain outside the display deadline.

## Storage Boundary

Storage is async relative to the frame.

Frame participants may:

- enqueue storage work
- poll completed storage work
- apply completed results within a budget
- render pending/stale/error states

Frame participants may not:

- block on file reads
- block on file writes
- perform unbounded filesystem traversal
- page fault unpredictably in field eval
- read storage inside pure field functions

Representative shape:

```wrela
fn editor_tick(editor: Editor, input: InputFrame, frame: FrameGraph) {
    editor.apply_input_now(input)

    while let Some(result) = editor.storage.completed.try_take(max_count = 4) {
        editor.apply_storage_result(result)
    }

    editor.layout_visible_region()
    editor.emit_fields(frame)
}
```

Network follows the same rule. The UI side of a network app is realtime. The
socket/download/protocol side is async.

## Business Logic Placement

Business logic splits by boundedness.

Frame-side logic:

- keystroke changes
- selection
- caret movement
- button pressed state
- small validation
- local command routing
- visible layout updates
- animation state
- field emission

Async/job-side logic:

- storage
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

## Multi-Display

The desktop should model display outputs explicitly even if the first hardware
path exposes only one.

Representative shape:

```wrela
data DisplayOutput {
    identity: DisplayIdentity
    physical_rect: Rect
    mode: DisplayMode
    scale: F32
    framebuffer: Framebuffer
}

data DesktopSpace {
    outputs: Slice<DisplayOutput>
    virtual_bounds: Rect
}
```

Fields live in desktop space. Each display output renders the subset of fields
that intersects its physical/virtual region.

The first milestone may support:

```text
one output guaranteed
multiple GOP outputs opportunistic
later GPU driver output enumeration
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
- vector width used
- cache hits and misses
- support rejections
- clip rejections
- layer cache use
- shadow cache use
- scaling/blit cost
- present cost
- app tick costs by visible app
- storage/network/job completions consumed this frame
- input-to-present latency estimate

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
  vector_path: avx2
  missed_reason: app.text_editor.syntax_visible
```

The report turns frame drops into fixable facts.

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

This is an escape hatch, not the normal foreground contract.

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

The first desktop milestone should prove the smallest coherent version:

```text
1. GOP framebuffer output
2. internal 1080p surface
3. display-paced frame clock with rate matching
4. flat field list
5. 2D field contracts with support, semantics, eval, shade
6. tiled CPU renderer with AVX2 first, AVX-512 path later
7. layered panels, text boxes, glyph fields, caret, selection
8. scroll view transform and clipping
9. input targets and pointer capture
10. global desktop frame loop
11. async storage boundary for visible app state
12. frame cost report
```

The first visual demo should show:

- a field-rendered desktop background
- at least one beautiful text box
- white field-rendered text on a gray rounded field panel
- cursor movement
- text selection
- scrolling
- a simple 3D field desktop object or background element
- a frame report that explains the cost of the scene

## Open Questions

- How much text shaping should be implemented before the first demo versus
  stubbed with generated ASCII glyph SDFs?
- Should the first renderer use 16x16 tiles, 32x8 tiles, or a measured choice
  based on vector packet shape and cache behavior?
- Should AVX2 be implemented before AVX-512 to ensure the fallback tier is real
  from day one?
- How should field function pointers or method references lower in Wrela's
  early compiler without adding dynamic dispatch?
- What is the first 3D field object that is delightful enough to justify the
  cost without distracting from text clarity?
- What minimum frame report format should become stable enough for tests?
- How should repeated foreground deadline misses be surfaced to the user during
  early development?

## Summary

Wrela's desktop should be a single realtime field world.

The model is:

```text
sample input
tick visible bounded app logic
poll async completions without waiting
emit fields and input targets
bin fields into tiles
evaluate vector packets
compose layers
present at hardware cadence
report every cost
```

This preserves the philosophical core:

- no hidden renderer
- no ambient GUI runtime
- no blocking frame lane
- no meshes as visual truth
- no mystery frame drops

The machine should feel awake because the whole foreground stack is organized
around the next visible frame.
