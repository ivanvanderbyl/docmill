# Observability — OpenTelemetry → VictoriaMetrics + VictoriaTraces

docmill is instrumented with OpenTelemetry (traces + metrics). The library
packages (`pkg/pdf`, `pkg/pdfiumcore`) record through the **global** OTel API,
which is a no-op until the CLI installs exporters — so there is zero overhead and
no behaviour change when telemetry is off.

## Enable it

Telemetry turns on when `DOCMILL_OTEL` is truthy **or** any `OTEL_EXPORTER_OTLP_*`
endpoint is set. Point metrics at VictoriaMetrics and traces at VictoriaTraces
(direct OTLP/HTTP — no collector required):

```bash
export DOCMILL_OTEL=1
export OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://localhost:8428/opentelemetry/v1/metrics
export OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://localhost:10428/insert/opentelemetry/v1/traces

go run ./cmd/docmill path/to/input.pdf
# or, with Task:
task obs:run -- path/to/input.pdf
```

The CLI flushes telemetry on exit, so even one-shot runs deliver their data.

### Delta temporality (why CLI metrics work)

docmill is usually a short-lived process. Many one-shot runs export once and
share identical labels, so **cumulative** temporality collides into a flat
counter and `rate()` returns nothing. The exporter therefore defaults to **delta**
temporality, which VictoriaMetrics records per-push; query it with `sum_over_time`
(see the queries below). Set
`OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE=cumulative` to override (useful
for a long-running server).

## What is emitted

Spans: `docmill.convert` → `docmill.page` → `pipeline.reading_order`,
`pipeline.table_detect`, `pipeline.assemble`, `render.table`, plus `pdfium.open`
and `pdfium.text_cells`. The `bench` subcommand adds a `docmill.bench.case` span
per backend.

Metrics (names keep dots in VictoriaMetrics; histograms expose `_bucket`/`_sum`/`_count`):

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `docmill.convert.duration` | histogram (s) | — | whole-document conversion |
| `docmill.page.duration` | histogram (s) | — | per page |
| `docmill.stage.duration` | histogram (s) | `stage` | per pipeline stage |
| `docmill.pages.processed` | counter | — | pages |
| `docmill.tables.detected` | counter | — | tables |
| `docmill.page.textcells` | histogram | — | text cells per page |
| `docmill.output.bytes` | histogram (By) | — | Markdown size |
| `pdfium.open.duration` | histogram (s) | `backend` | PDF open |
| `pdfium.text_cells.duration` | histogram (s) | `backend` | structured text extraction |
| `pdfium.text_cells.count` | histogram | `backend` | cells extracted |

Duration histograms use latency-tuned bucket boundaries (~50µs–2.5s); the default
OTel boundaries are seconds-scale and would collapse every sub-millisecond stage
into one bucket.

## Grafana dashboard

Import `grafana-dashboard.json` (Dashboards → New → Import) and select your
VictoriaMetrics datasource when prompted. Panels: per-stage avg/p95 latency, total
time share by stage, throughput, and text-extraction latency. Traces are in
VictoriaTraces (`select/vmui`, or a Grafana traces datasource) under service
`docmill`.

## Useful PromQL/MetricsQL (delta data → `sum_over_time`)

```promql
# Average time per pipeline stage
sum by (stage)(sum_over_time(docmill.stage.duration_sum[$__range]))
  / sum by (stage)(sum_over_time(docmill.stage.duration_count[$__range]))

# p95 per stage
histogram_quantile(0.95, sum by (stage, le)(sum_over_time(docmill.stage.duration_bucket[$__range])))

# Text extraction p95 (the dominant cost)
histogram_quantile(0.95, sum by (backend, le)(sum_over_time(pdfium.text_cells.duration_bucket[$__range])))
```

## What the data shows (find areas to improve)

On the bundled corpus, PDFium text extraction (~2–5 ms/page) dwarfs the entire Go
pipeline (reading-order/table-detect/assemble/render together ≈ tens of µs/page).
Within the pipeline, `table_detect` and `render.table` are the most expensive
stages; `reading_order` is cheapest. So optimisation effort is best spent on text
extraction, not the deterministic Go stages.
