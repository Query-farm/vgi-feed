# Executed feed coverage walkthrough

[Does an item cap preserve recent entries?](01_measure_feed_prefix_coverage.ipynb) fetches explicitly synthetic RSS, Atom and JSON Feed documents through the real released Go worker, verifies common-field and HTTP/inline agreement, and measures the recent entries missed by a source-order cap. SQL owns parsing calls, time classification, joins and coverage arithmetic; Python serves fixture bytes and presents results.

## Install and run

Requirements: [uv](https://docs.astral.sh/uv/), Python 3.13 or 3.14, and initial network access for dependencies and the released binary. No Go installation, worker checkout, service account or global kernel is required. Copy this entire directory to run independently.

```sh
cd notebooks
uv sync --frozen
uv run --frozen python execute.py --timeout 180
```

The separate lock pins published Haybarn 2.0.0rc11. The runner creates a temporary kernel using its own interpreter and notebook-directory cwd. It saves atomically only after all cells succeed. Root Go dependencies and locks remain unchanged.

For interactive editing:

```sh
uv run --frozen python -m ipykernel install --user --name vgi-feed-notebooks --display-name 'Python (feed notebooks)'
```

Select that kernel with cwd here, edit the visible parameter, restart and run all. Python support and plot cells are folded; connection and analytical SQL remain visible. Official branding is local and unchanged.

## One-parameter experiment

`MAX_ITEMS=4` accepts integers 1–10 and limits emitted entries in source order. Run a separate cap-8 copy:

```sh
uv run --frozen python execute.py --parameters '{"MAX_ITEMS":8}' --output-dir artifacts/cap-eight
```

Overrides require a separate destination; failures preserve the successful source notebook. The runner copies branding, fixtures, helpers, README and environment files beside experiment copies. Reset 4 for defaults. Logs, chart extracts, caches and experiment copies are ignored.

## Portable launch and synthetic provenance

```sql
INSTALL vgi FROM community;
LOAD vgi;
ATTACH 'feed' (TYPE vgi, LOCATION 'github-auto://Query-farm/vgi-feed@v0.5.0');
```

The extension resolves a pinned published platform asset and verifies its sidecar checksum. This location was executed through published Haybarn on macOS arm64. To upgrade, explicitly change the tag and rerun defaults, the experiment and checks. No local build substitutes for the published worker.

[Fixture notes](fixtures/README.md) describe ten fictional bulletin entries encoded in three formats with identical common fields and nonchronological order. A read-only loopback HTTP server exposes those documents to the worker; an exact nine-request audit checks one metadata, uncapped and capped call per format. Inline RSS parsing makes no HTTP request. The actual worker performs fetching, feed detection, date parsing and Arrow serialization. This is synthetic HTTP validation, not recorded or live publisher data. No publisher endpoint, entry link or external account is accessed by the analysis.

The fixed UTC window is [2026-01-13 12:00, 2026-01-15 12:00). Unknown timestamps are classified separately; the fixtures have none. One entry is exactly at the excluded anchor and one is later. The worker emits plain TIMESTAMP values, interpreted here as the authored UTC instants. Topic labels overlap and cannot be summed as disjoint entry counts.

`max_items := 0` uses a one-million-item ceiling, not infinite history. The worker fetches and parses before slicing; this notebook measures returned-row coverage, not reduced HTTP traffic or latency. Comparisons cover common normalized fields, not lossless format conversion or strict standards validation. COPY/write functions are outside this read-only investigation.

## Validation

Verified through published Haybarn on macOS arm64:

- Defaults and cap-eight experiment execute from a copied notebooks-only directory in 1.8 s each. Each has 16 code cells, seven visible SQL cells, two charts, ten SQL invariants and the exact nine-request HTTP audit. All four charts were visually inspected.
- Cap 4→8 raises recent recall 2/6→5/6 (33.3%→83.3%), leaving four→one recent entries omitted. At cap eight, data / operations / release recall is 100% / 66.7% / 83.3%; all document counts, dates and uncapped results stay fixed.
- All three runner guards preserve the successful default. Structural review, Ruff check/format and unchanged official branding pass.
- Required source gates pass: 48 Go tests, 131 SQL assertions in four cases, go vet, golangci-lint (zero issues) and gofmt. Metadata scores 100 L2 with zero findings at fail-on-info; execution enabled, external-link and paid AI passes disabled.

Both root Go manifest and lock are unchanged. Generated logs and extracted charts are under ignored `artifacts/`. The released tag resolves to `ee87ffa05e70fd1a91a36429aed7ca904375163d`.
