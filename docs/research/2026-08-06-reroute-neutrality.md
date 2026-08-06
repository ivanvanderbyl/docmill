# Task 5: the classify-then-route pipeline, and its neutrality gate

**Date:** 2026-08-06
**Plan:** `docs/plans/2026-08-06-learned-layout-classifier.md`, Task 5
**Follows:** `2026-08-06-heuristic-baseline-and-features.md`
**Verdict:** **Gate passed.** The rerouted path is byte-identical to the default path on
all 200 DPBench documents. The residual risk Task 6 inherits is now a number:
**0.847%** of class-agnostic lines straddle a routing boundary.

---

## What was built

`ExtractionOptions.ClassifyThenRoute` selects an alternate pipeline in
`pkg/pdf/reroute.go`. The default path is untouched and the flag is off by default.

The restructure exists because today's order decides tables *before lines exist*:
`DetectTables` consumes cells directly and produces table blocks whose cells never reach
the line assembler, and the heading detector splits its cells out earlier still. A line
classifier bolted onto today's assembly cannot take over those decisions — they are
already baked into what got assembled. The new order is:

1. assemble **all** cells into lines, class-agnostically
2. route each line to a destination
3. hand each destination's cells to the existing builder

**Every routing decision in this task is still the existing heuristics'.** Nothing
learned is in the loop. That is the point: it isolates refactor risk from model risk, so
the reroute can be proven neutral before any model decision goes live.

Table detection was lifted into a shared `detectPageTables` used by both paths, so
"the reroute changes the ORDER, not the decisions" is enforced by there being one
definition rather than asserted in a comment.

## The gate

The plan requires DPBench through the rerouted path to match the current pipeline within
noise. `benchmarks/layout/spike/cmd/reroutecheck` runs both paths over the corpus in one
process and compares rendered Markdown:

```
documents: 200   identical: 200   differing: 0   errors: 0
GATE PASSED: the rerouted path is byte-identical to the default path.
```

Byte-identity is strictly stronger than matching DPBench scores — every metric the
harness computes is a function of this output, so all of them are unchanged by
construction. Running the scoring harness would add no information.

Five unit tests in `pkg/pdf/reroute_test.go` pin the same property on prose, lists,
tables, multi-page documents and empty pages, so a regression fails `go test` rather than
waiting for someone to remember a corpus run.

## The part of the gate that is honest about itself

**Byte-identity here is partly by construction, and it is worth being precise about
which part.** The rerouted path computes the class-agnostic line set, but it rebuilds
prose blocks from the *routed cells*, not from those lines. So the new assembly exists
and is available to the classifier; it does not yet build output.

That was a deliberate choice. A line assembled from every cell on the page can straddle a
table gutter or merge a heading with body text beside it — the plan's stated hazard,
"class-agnostic assembly creates lines that never existed before". Routing lines directly
would have conflated the refactor's risk with that hazard's, and the whole purpose of
Task 5 is to keep them apart.

So the gate proves the *plumbing* is neutral. It does not prove that routing lines
directly is neutral, because that is not what this task does.

## The number Task 6 inherits

To stop that being an unquantified unknown, the harness also measures how many
class-agnostic lines are only *partly* inside a routing region — neither cleanly in nor
cleanly out (containment between 0.05 and 0.95). Those are exactly the lines whose output
would change the moment routing is done on lines rather than cells.

```
class-agnostic lines straddling a routing boundary: 52 of 6137 (0.847%)
```

Under one line in a hundred. Samples show the expected shapes:

| containment | destination | line |
|---|---|---|
| 0.27 | heading | `1. Introduction and Methodology 2. General Profile of MSMEs` |
| 0.29 | heading | `Number of GSDP` |
| 0.36 | heading | `MOHAVE COMMUNITY COLLEGE BIO181` |
| 0.19 | table | `26` |

The first is the archetype: two column headings merged into one class-agnostic line
because nothing carved the columns apart before assembly. These are column-adjacency
artefacts, not a systemic assembly failure — which is the good outcome, and it means
Task 6 can route lines directly provided it handles this ~1% deliberately rather than
discovering it in a DPBench regression.

## Shadow mode

`SetShadowRouteSink` collects, for every class-agnostic line, the destination the
heuristics sent it to — changing nothing. It is nil in production, so shadow mode costs a
routing pass and nothing else. Once the model is embedded, the same hook records what it
*would* have decided, which is the live confusion matrix that cross-checks the offline
one from Task 1.

The predictors are not wired in yet: the DocLayNet model is 32-feature multiclass and the
embedded artefact is still the Task 0 binary Formula model. Wiring it is a small step,
and it belongs with the storage-form decision (Go literals vs `go:embed`ed blob) already
measured in the codegen note.

## What is still not measured

Nothing here has changed a single character of docmill's output — by design. Task 5's
success condition is precisely that nothing changed. The first real quality signal comes
in Task 6, when `Formula` routing switches to the model and DPBench is run for a delta
rather than for identity.
