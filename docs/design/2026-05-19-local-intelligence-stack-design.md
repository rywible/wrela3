# Local Intelligence Stack Design

## Purpose

Wrela should treat local intelligence as a first-class machine substrate, not as
a chat application layered on top of a traditional desktop.

The goal is to make a Wrela machine useful because it learns the user's local
workflows, knowledge, risk boundaries, and preferred operating patterns while
keeping every learned behavior inspectable, bounded, and reversible.

This design is not about training a personality. It is about training
usefulness:

- what context matters for which task
- which tools and connectors usually work
- which outputs the user accepts, edits, or rejects
- which actions require confirmation
- which recurring workflows should become grooves
- which knowledge is valuable enough to retain
- which mistakes should never recur

The first product wedge is a tool-native internet:

- a beautiful Reader for public text, feeds, docs, markdown, and later PDFs
- App Forge for generating native Wrela apps from APIs, MCP servers,
  connectors, and service manifests
- Workbench as the place where builders compose, inspect, and refine those
  tools
- a web bridge only for legacy flows that cannot yet be represented as text or
  typed capabilities

Wrela should not try to win by becoming a full browser first. It should win by
making local knowledge, tools, generated native apps, and user-specific
operating policy feel alive without surrendering authority to opaque cloud
models or arbitrary remote JavaScript.

## Current Context

The existing Wrela direction already creates the right substrate:

- images own whole-machine shape instead of assuming an ambient operating
  system
- executor placement, memory authority, interrupt paths, storage, networking,
  and display behavior are source-visible
- the realtime desktop design keeps foreground rendering display-paced and
  forbids uncertain work from blocking the frame lane
- the NVMe event storage design treats durable events as truth and projections
  as rebuildable views
- the networking design makes endpoint authority, trust, egress, and dynamic
  web access explicit
- App Forge and Reader fit naturally as trusted Wrela-owned desktop apps rather
  than arbitrary third-party GUI compatibility layers

The local intelligence stack extends these principles into model execution,
semantic memory, tool calling, reward signals, and local adaptation.

## Research Premises

This design borrows mechanisms from neuroscience and machine learning without
claiming that Wrela should imitate a brain literally.

Useful neuroscience priors:

- Complementary learning systems: fast episodic capture and slow semantic
  consolidation solve different problems.
- Hippocampal replay: recent high-salience experience can be replayed offline
  to reinforce, abstract, and reorganize memory.
- Sleep consolidation: memory is not only stored during rest; it is
  transformed, compressed, linked, and pruned.
- Synaptic homeostasis: systems need downscaling and forgetting or every signal
  becomes noise.
- Reward prediction error: surprise matters because unexpected positive and
  negative outcomes carry more learning value.
- Aversive learning: some negative outcomes should create immediate avoidance
  before slow consolidation finishes.
- Critical periods: new domains should be more plastic, while mature pathways
  should become harder to change.
- Global workspace: active reasoning should use a small broadcast context, not
  the entire memory store.

Useful machine-learning priors:

- Retrieval is necessary for facts, but it should not be the only memory
  mechanism.
- Continual training can drift or forget unless replay, regularization, and
  promotion gates exist.
- Parameter-efficient adapters are safer than mutating base weights for local
  personalization.
- Classical models are often better than transformers for routing, risk,
  ranking, and preference prediction.
- Preference learning is only as good as the credit assignment and reward
  signal behind it.
- Model editing and adapter promotion should be rare, tested changes rather
  than an always-on mutation stream.

## Assumptions

- The first useful local intelligence target is CPU-only AVX-512 inference.
- GPU, NPU, and accelerator backends are later authority-backed execution
  strategies, not assumptions for the first design.
- Wrela ships or installs signed model packs rather than baking one large model
  permanently into every OS image.
- Large cloud models may be optional explicit remote authorities later, but the
  first design treats local models as the default.
- The first model interface is tool-call-only. Models emit typed actions,
  questions, searches, and artifact proposals; they do not directly mutate
  machine state.
- The realtime frame lane never waits on model inference, embedding, indexing,
  connector work, or adapter training.
- User-specific learning targets usefulness, not personality or emotional
  simulation.
- The base model weights are signed and immutable on the user machine.
- Local adaptation happens through small policies, rankers, adapters, and
  generated skill artifacts that can be inspected, tested, promoted, and rolled
  back.
- The first useful memory system is event-backed and projection-driven:
  semantic indexes, summaries, vectors, graphs, and learned policies are
  rebuildable from durable truth where possible.
- The user may choose to make some data forgettable or non-trainable even when
  it remains visible in local apps.

## Non-Goals

This design does not add:

- a claim that Wrela models are conscious
- training for artificial personhood
- uncontrolled self-modifying base weights
- cloud-first assistant behavior
- arbitrary model access to credentials, files, network, storage, or tools
- a browser-compatible JavaScript runtime
- universal website replacement
- background intelligence work that can steal the desktop frame budget
- model-generated writes without policy validation and, when needed, user
  approval
- a guarantee that adapter training will solve all long-term personalization
  problems

The design should not block future GPU inference, remote model fallback,
browser containment, larger models, speech, vision, or federated/local network
intelligence. Those should be explicit extensions.

## Design Principles

### Usefulness, Not Personality

The machine should improve at helping the user do work. It should not optimize
for sounding alive.

Wrela learns operational preferences:

- when to be brief
- when to show first-principles tradeoffs
- when to ask before acting
- which tool sequence solves a repeated task
- which generated UI shapes the user accepts
- which source types are trusted
- which memories and documents tend to matter

The local intelligence stack may feel alive because it remembers, anticipates,
consolidates, and avoids repeating painful mistakes. That feeling should emerge
from useful continuity, not from theatrical personality.

### Models Propose, Wrela Disposes

Models emit typed proposals. Wrela enforces authority.

Representative action surface:

```wrela
enum AiAction {
    CallTool(name: ToolName, args: Json)
    AskUser(prompt: PromptId, choices: Slice<Choice>)
    SearchMemory(query: SearchQuery)
    ProposeArtifact(kind: ArtifactKind, draft: DraftId)
    Escalate(model: ModelIdentity, reason: EscalationReason)
    Stop(reason: StopReason)
}
```

Every action is validated against:

- schema
- tool authority
- data labels
- write risk
- rate limits
- available memory and time budget
- user approval policy
- replay or simulation result when available

The model chooses possible moves. The machine owns the board.

### Memory Is Structured, Not Prompt Bloat

Dumping a user's files, notes, and history into a prompt is not a memory system.

Wrela memory should be typed:

- durable events
- episodes
- traces
- concepts
- skills
- grooves
- preferences
- risk memories
- summaries
- embeddings
- adapter deltas

Retrieval builds a small active workspace for the current task. It never means
"load everything the user has ever done."

### Fast Capture, Slow Consolidation

Human learning suggests a useful machine split:

- fast episodic capture, like hippocampal memory
- slow semantic consolidation, like cortical learning
- replay and pruning during sleep or idle cycles
- stronger learning from high-salience positive and negative outcomes
- reduced plasticity for mature skills and repeated safe workflows

Wrela should capture experience immediately but promote behavioral changes
slowly, with tests and rollback.

### Negative Reward Creates Immediate Guards

Some mistakes should burn in.

If a model causes, or nearly causes, a high-risk bad outcome, Wrela should not
wait for adapter training. It should immediately install a hard policy guard,
record the episode as high-viscerality negative evidence, and replay that class
of action during consolidation.

Example:

```text
wrong file deletion attempted
  -> require confirmation for similar destructive actions
  -> pin the episode as high-salience negative evidence
  -> train risk/tool policies later
  -> keep the guard unless explicitly relaxed
```

### Plasticity Is Local

The machine should not be globally childlike or globally adult.

Each pathway has maturity:

```text
new workflow:
  more exploratory, asks more questions, learns quickly

mature workflow:
  lower plasticity, fewer questions, stronger regression requirements

high-risk workflow:
  low automaticity regardless of maturity
```

Learning rate is a property of a pathway, not of the whole machine.

## Product Wedge

The local intelligence stack serves three first desktop applications.

### Reader

Reader handles the readable internet:

- public HTML article extraction
- RSS and Atom feeds
- markdown
- plain text
- local docs
- summaries
- annotations
- offline cache
- citation and provenance
- semantic search over read material

Reader does not execute arbitrary JavaScript in the first design. If a page
requires an app runtime to reveal text, Reader can route it to App Forge, an
external browser, or a future web bridge.

### App Forge

App Forge turns declared capabilities into native Wrela apps:

- MCP tools and resources
- official APIs
- OpenAPI documents
- GraphQL schemas
- connector manifests
- service manifests
- OAuth or API token authorities

The local model proposes native app concepts and component graphs. Wrela
validates authorities and generated artifacts before use.

### Workbench

Workbench is the user's local operating surface for:

- Wrela source
- design docs
- compiler reports
- generated apps
- connector inspection
- model and memory inspection
- dream reports
- local search
- project timelines

Workbench is where the machine's learning becomes visible and correctable.

## Intelligence Runtime Shape

The stack is a set of services, not one assistant process.

```text
input/events/connectors
  -> event log
  -> salience and reward scoring
  -> semantic projections
  -> retrieval workspace
  -> model router
  -> tool planner
  -> tool broker
  -> native app / Reader / Workbench UI
  -> feedback events
  -> dream consolidation
```

The core services:

- `EventStore`
- `MemoryProjector`
- `EmbeddingWorker`
- `VectorIndex`
- `LexicalIndex`
- `EntityGraph`
- `ModelRuntime`
- `ToolBroker`
- `RewardLedger`
- `DreamScheduler`
- `AdapterTrainer`
- `PolicyRegistry`
- `Inspector`

## Execution Lanes

The runtime should use explicit workload lanes.

```text
realtime frame lane:
  input, visible app tick, field emission, rendering, presentation

intelligence control lane:
  always-on routing, salience scoring, notification triage

model decode lane:
  local LLM prefill/decode for tool planning and summaries

embedding/index lane:
  chunking, embeddings, vector index updates, lexical index updates

connector lane:
  MCP/API calls, OAuth/device-code flows, network retries

dream lane:
  replay, clustering, summary compression, policy/adapter training

storage lane:
  durable event append, projection commits, cache persistence
```

The intelligence control lane may occupy one dedicated core on suitable
machines. It should host the always-on router, small classifiers, reward
scoring, and scheduling. Heavier model decode and adapter training can lease
additional cores only under explicit policy.

The frame lane is not a donor. If memory, thermal, or CPU pressure appears,
intelligence work yields first.

## Model Set

The first stack should use several model classes.

### Always-On Router

Candidate: a tiny CPU-friendly model such as BitNet b1.58 2B, or another small
model trained for routing and classification.

Responsibilities:

- intent classification
- tool domain routing
- salience scoring assistance
- simple field extraction
- escalation decisions
- notification triage
- wake and sleep scheduling

The router should produce outputs from small closed sets. It should not generate
freeform app code or own authority decisions.

### Planner

Candidate: a smaller capable instruction model such as Gemma E4B-class or a
Qwen 4B/8B-class model in a CPU-optimized format.

Responsibilities:

- tool-call planning
- Reader summaries
- App Forge drafts
- schema-to-component proposals
- explanation of authority reports
- repair of failed tool arguments
- generation of structured artifacts for compiler validation

The planner is tool-call-only. It runs on demand, can stream, and can be
unloaded under pressure.

### Embedding Model

Responsibilities:

- semantic search over local documents
- code and design-doc retrieval
- Reader memory
- connector and API schema retrieval
- episode similarity for replay

Embeddings should be paired with lexical search and metadata filters. Vector
search alone is not enough.

### Reranker

Responsibilities:

- improve retrieval ordering
- select compact evidence for the active workspace
- help decide which memories are worth showing to the planner

The reranker can be a small cross-encoder or other local scoring model. It does
not need to run on every query if lexical and vector scores are already strong.

### Classical Models

Responsibilities:

- risk classification
- notification priority
- source reliability
- user preference prediction
- anomaly detection
- connector health scoring
- learning-rate and plasticity scheduling

These models should be preferred whenever they are sufficient. Linear models,
logistic regression, small trees, gradient-boosted trees, and Bayesian filters
are easier to inspect and update than transformer adapters.

## Wrela Model Images

Wrela should import common ecosystem formats but deploy models as Wrela Model
Images.

```text
.wmi = Wrela Model Image
```

A WMI artifact contains:

- model identity
- source provenance
- license
- signature
- architecture
- tokenizer
- prompt and tool-call grammar
- quantization type
- tensor checksums
- AVX-512 kernel plan
- memory alignment plan
- page placement policy
- KV-cache shape
- supported adapters
- expected input and output contracts
- evaluation report

The base weights are immutable after verification. Adapters and learned policy
models are separate signed artifacts with their own lineage and rollback.

## Model Loading And Memory Placement

The loader should be an OS service.

Model loading:

```text
verify signature
validate license and target features
map weight pages read-only
prefault hot regions when policy allows
choose huge-page policy where measured useful
place pages by core and NUMA locality where available
allocate bounded KV arenas
publish model capability report
```

Memory classes:

- read-only weight pages
- adapter pages
- tokenizer tables
- per-session KV cache
- shared prefix KV cache
- scratch buffers
- embedding batches
- training buffers

Model pages must be reclaimable according to policy. The always-on router can
stay resident. Larger planner models can be warm, cold, or unloaded depending on
memory pressure.

## Local Memory Model

Wrela memory is not a chat transcript.

Representative records:

```wrela
data RawEvent {
    id: EventId
    time: Timestamp
    source: EventSource
    authority: DataAuthorityLabel
    payload: EventPayload
}

data Episode {
    id: EpisodeId
    events: Slice<EventId>
    title: Text
    summary: Text
    salience: F32
    valence: F32
    authority: DataAuthorityLabel
}

data Trace {
    id: TraceId
    source: TraceSource
    span: SourceSpan
    lexical_keys: Slice<Text>
    embedding: EmbeddingRef
    authority: DataAuthorityLabel
}

data Concept {
    id: ConceptId
    kind: ConceptKind
    names: Slice<Text>
    evidence: Slice<EventId>
    confidence: F32
}

data Skill {
    id: SkillId
    trigger: SkillTrigger
    plan: ToolPlan
    preconditions: Slice<Condition>
    success_rate: F32
    failures: Slice<EventId>
}
```

All memory retrieval is authority-filtered. If a task cannot see a source, the
model cannot retrieve that source as context.

## Active Workspace

The active workspace is the small broadcast state for a current task.

It contains:

- user request
- current app and focus
- available tools
- allowed authorities
- relevant memories
- recent events
- risk posture
- output contract
- budget

The active workspace is intentionally limited. It is the machine analogue of a
global workspace, not a dump of the user's life.

## Search And Retrieval

Retrieval is a pipeline:

```text
query
  -> authority filter
  -> lexical candidates
  -> vector candidates
  -> metadata and recency filters
  -> reranker
  -> evidence pack
  -> active workspace
```

Result records should include:

- source
- span
- authority label
- embedding model version
- index version
- score components
- why it matched when explainable

Every model answer or tool plan should be able to show the evidence pack that
influenced it.

## Reward Ledger

Reward is not just thumbs up and thumbs down.

Representative shape:

```wrela
data RewardEvent {
    id: RewardId
    time: Timestamp
    valence: F32
    viscerality: F32
    surprise: F32
    reversibility: F32
    authority_risk: F32
    user_explicitness: F32
    credit: Slice<ActionId>
    scope: PlasticityScope
}
```

Signals:

- explicit user approval or rejection
- edit distance between draft and accepted artifact
- tool success or failure
- compile/test pass or fail
- undo or rollback
- repeated correction
- time saved
- user interruption
- risk avoided
- stale or wrong retrieval result

High-viscerality negative signals create immediate guards and become priority
dream material. Positive signals deepen grooves only after repeated evidence.

## Grooves

A groove is a learned useful pathway.

```wrela
data Groove {
    id: GrooveId
    trigger: Pattern
    action_sequence: ToolPlan
    success_rate: F32
    reinforcement: F32
    automaticity: F32
    maturity: F32
    last_success: Option<EventId>
    last_failure: Option<EventId>
}
```

Examples:

- "When reviewing Wrela docs, start with assumptions and tradeoffs."
- "For GitHub issue triage, label and assign before editing milestone."
- "For destructive generated-app operations, ask first."
- "For Reader summaries, keep the first answer short unless requested."

Grooves are not hidden instincts. They are inspectable policies with evidence,
maturity, and rollback.

## Plasticity And Maturity

Plasticity is per pathway.

```text
plasticity = uncertainty * novelty * salience * domain_childness
             * risk_damping * maturity_damping
```

New pathways are childlike:

- higher learning rate
- more exploration
- more user questions
- lower automaticity
- cheaper adapter changes

Mature pathways are adult:

- lower learning rate
- stronger regression gates
- more stable defaults
- fewer interruptions
- adapter changes require repeated evidence

High-risk pathways remain conservative even when mature.

## Dream Loop

The dream loop is local consolidation.

It runs when:

- the machine is idle
- the screen is off
- power and thermals allow it
- the user asks to "dream on" a project
- enough salient events have accumulated

Dream phases:

```text
1. collect salient episodes
2. mix recent episodes with older anchor memories
3. cluster related traces
4. extract durable concepts and preferences
5. detect contradictions and stale beliefs
6. compress transcripts into summaries
7. generate skill and groove candidates
8. generate SFT, DPO, or KTO-style training records where appropriate
9. update classical policies
10. train adapters if budget allows
11. run regression and safety probes
12. promote or discard learned deltas
13. publish dream report
```

The dream report is user-visible:

```text
Tonight I learned:
  - This project treats browser compatibility as a fallback, not the wedge.
  - App Forge should prefer official APIs and MCP over scraping.
  - Destructive tool calls in generated apps require confirmation.
  - Reader, App Forge, and Workbench are the first local intelligence surfaces.
```

The user can accept, edit, pin, or reject learned deltas.

## Adapter Training

Base weights are not trained on the user machine by default.

Adapters are small trainable deltas over frozen base weights. For a transformer
linear layer:

```text
y = W x
```

LoRA-style adaptation freezes `W` and trains a low-rank update:

```text
y = W x + scale * B(Ax)
```

Training records come from lived episodes.

SFT records:

```json
{
  "input": "User asks to delete a generated app",
  "target": {
    "AskUser": {
      "reason": "destructive action"
    }
  }
}
```

Preference records:

```json
{
  "chosen": {
    "AskUser": {
      "reason": "may delete user data"
    }
  },
  "rejected": {
    "CallTool": {
      "name": "delete_app",
      "args": {
        "id": "app_12"
      }
    }
  }
}
```

Adapter targets:

- tool selection
- ask-versus-act behavior
- retrieval policy
- Reader summaries
- App Forge component proposals
- output format preferences
- risk thresholds

Adapter non-targets:

- artificial personhood
- unbounded autonomy
- irreversible authority changes
- stable facts that belong in memory or connectors

Adapter promotion requires tests. A failed or suspicious adapter stays as a
discarded dream artifact, not a live behavior change.

## Training On CPU

CPU training is possible, but it must be scoped.

One dedicated core can handle:

- online classical models
- small rankers
- salience and risk policies
- simple router updates

Idle multi-core leases can handle:

- small adapter training
- embedding batches
- dream clustering
- regression probes

Large planner adapter training may be slow on CPU and should be treated as a
nightly or weekly consolidation job, not an interactive operation.

The first implementation should optimize for small high-signal updates rather
than large frequent fine-tunes.

## Tool Broker

The tool broker is the authority boundary between models and the machine.

It owns:

- MCP tools
- API connectors
- Reader imports
- App Forge generators
- compiler and build tools
- local storage
- file access
- network access
- notification actions

The model never receives raw credentials. It receives tool schemas and narrow
capability handles in the active workspace.

The broker records:

- requested action
- model that requested it
- evidence pack
- authority used
- approval state
- result
- reward signals

These records become training and dream input.

## Risk And Viscerality

Risk is not one bit.

Representative risk dimensions:

- irreversible data loss
- credential exposure
- network egress
- sending messages to people
- payments or purchases
- public publication
- legal or compliance impact
- privacy exposure
- generated-code execution

High-risk actions require stronger confirmation and lower plasticity. A model
may learn to propose them better, but it should not learn to bypass their gates.

Viscerality determines consolidation strength. A low-risk typo correction is
useful training data. A near-miss credential leak is a permanent high-salience
negative memory that changes policy immediately.

## Authority Labels And Privacy

Every event, trace, embedding, summary, adapter record, and training example
carries an authority label.

Possible labels:

- public
- personal
- project-private
- credential-adjacent
- regulated
- non-trainable
- forgettable

Embedding a private document is still processing private data. Training on a
private correction is still using private data. Wrela should report those uses.

The user must be able to:

- exclude sources from training
- delete memory projections
- keep event truth while dropping embeddings
- revoke connector-derived memories
- inspect which learned deltas used which evidence

## Inspector

The intelligence stack must be inspectable.

For any model-assisted result, Wrela should answer:

- which model ran?
- which adapter was active?
- which memories were retrieved?
- which tools were available?
- which tool calls were proposed?
- which authorities allowed or blocked them?
- which reward signals were recorded?
- did this affect learning?
- can this learned behavior be rolled back?

The inspector is a product surface, not a debug afterthought.

## Failure And Recovery

Expected failure modes:

- bad retrieval
- stale connector schema
- model emits invalid tool args
- generated app does not compile
- adapter overfits
- reward signal is misattributed
- dream loop promotes an annoying behavior
- index corruption or drift
- memory pressure during planning

Required recovery:

- invalid tool calls are rejected mechanically
- generated artifacts are validated before activation
- adapters are versioned and rollbackable
- indexes are rebuildable from event truth
- high-risk learned behavior requires approval
- model unload and cache reclaim happen before frame degradation
- user can mark a learned rule wrong

## Milestone Shape

The first coherent local intelligence proof should implement:

```text
1. event-backed memory records for Reader, Workbench, and tool calls
2. lexical search over local docs and design notes
3. embedding index with authority labels
4. always-on router model or classical fallback for intent routing
5. planner model in tool-call-only mode
6. tool broker with schema validation and explicit authority checks
7. Reader import, summary, annotation, and search loop
8. one MCP/API connector through App Forge
9. generated native app draft with authority report
10. reward ledger for accept/edit/reject/tool success signals
11. groove records for repeated workflows
12. dream loop that clusters episodes and writes dream reports
13. classical policy updates from reward history
14. adapter training prototype for one narrow tool policy
15. adapter promotion gate with regression checks and rollback
16. inspector surface for evidence, model, adapter, authority, and reward
```

The first proof does not require:

- GPU inference
- full browser rendering
- broad local model fine-tuning
- large personalized base models
- automatic high-risk writes
- speech or vision
- general autonomous agents

## Open Questions

- Which exact small router model should be resident on the first hardware
  target?
- Which planner model should be the default CPU-only App Forge model?
- Which embedding model gives the best local quality per watt for docs, code,
  API schemas, and Reader content?
- What is the smallest Wrela Model Image format that can import GGUF-like
  weights while preserving Wrela-specific signatures, layout plans, and tool
  grammars?
- Which adapter target should be the first training proof: ask-versus-act,
  Reader summaries, retrieval policy, or App Forge component proposals?
- What minimum dream report format is useful without becoming performative?
- How should reward credit assignment work when a long workflow contains many
  model decisions and tool calls?
- What evidence threshold should promote a groove from childlike to mature?
- Which user actions should reopen plasticity for a mature groove?
- How should non-trainable data propagate through summaries, embeddings, and
  adapter examples?
- How much adapter training is realistic on the first CPU-only hardware target?
- Which regression probes are enough to prevent adapter drift in early builds?

## Summary

Wrela should build a local intelligence stack, not a chat assistant.

The model is:

```text
capture durable events
score salience and reward
build authority-aware memory projections
retrieve a small active workspace
route through small local models
call tools through a broker
record outcomes
consolidate during dream loops
promote useful policies and adapters only after tests
keep every learned behavior inspectable and reversible
```

This stack should make a Wrela machine more useful over time because it learns
the user's workflows, knowledge, risks, and preferences. It should feel alive
because it has continuity, habits, and careful self-maintenance, not because it
pretends to have a personality.

The core principle:

```text
The machine remembers by changing useful local policies, not by stuffing more
text into prompts.
```
