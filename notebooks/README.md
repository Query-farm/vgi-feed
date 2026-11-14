# Feed Parser examples

[Worker repository](https://github.com/Query-farm/vgi-feed) · [Shared notebook tools and standard](https://github.com/Query-farm/vgi-example-notebooks)

| Notebook | Investigation | Run |
|---|---|---|
| [Does an item cap preserve recent entries?](01_measure_feed_prefix_coverage.ipynb) | Fetch the same synthetic bulletin in three formats and measure the recent items omitted by a source-order cap. | [Open in Colab](https://colab.research.google.com/github/Query-farm/vgi-feed/blob/main/notebooks/01_measure_feed_prefix_coverage.ipynb) |
| [Export order is not item identity](02_export_order_is_not_identity.ipynb) | Write RSS, Atom and JSON Feed, re-import each document, and distinguish stable IDs, regenerated positions and timestamp defaults. | [Open in Colab](https://colab.research.google.com/github/Query-farm/vgi-feed/blob/main/notebooks/02_export_order_is_not_identity.ipynb) |

Both notebooks contain saved results, visible SQL connections, linked worker
titles, Colab buttons and consistent Query Farm Strata Sun/VGI branding. Four SVG
figures remain sharp on GitHub. GitHub displays code expanded; shared setup and
small plotting helpers keep it concise. Editor hiding metadata is a convenience.

## Run locally or in Colab

Open either Colab link and run all cells. Locally, run from this directory:

```sh
uv sync --locked
uv run --locked vgi-notebooks --timeout 300
```

Select `.venv/bin/python` for an interactive kernel. Standalone setup installs
only the shared base package and preserves compatible existing kernel/plot
packages. The local environment additionally includes the execution runner.
The fixture server, presentation helper and authored documents are downloaded
from an immutable public revision and SHA-256 checked. No worker checkout or
copied execution/standard script is required.

Each notebook shows the released worker connection:

```sql
ATTACH 'feed' (TYPE vgi,
 LOCATION 'github-auto://Query-farm/vgi-feed@v0.5.0');
```

The VGI extension downloads the platform's public prebuilt release, verifies its
SHA-256 and caches it. Initial setup needs internet access. No Go compiler,
source build, SDK overrides or API credentials are required. The notebooks run
local computations and, in the first example, actual HTTP requests to a read-only
loopback fixture server. They do not fetch publishers, article links or images.

## Input and result scope

[Fixture notes](fixtures/README.md) describe ten fictional bulletin entries with
identical common fields encoded as RSS, Atom and JSON Feed in deliberately
nonchronological order. All timestamps denote UTC. Example links are identifiers,
not fetched resources. The worker's native feed directory is grouped by declared
format to discover those reader paths; directory metadata is not a live URL check.

The first notebook fixes the interval to **[13 January 2026, 12:00; 15 January
2026, 12:00) UTC**. Unknown dates, older entries and entries at/after the anchor
remain separate. Overlapping topic labels cannot be summed as disjoint populations.
An exact nine-request audit verifies one metadata, full and capped fetch per
format; inline RSS parsing makes no additional HTTP request.

`max_items := 0` uses a one-million-item safety ceiling, not unbounded history.
The worker fetches and parses before slicing. This example measures returned-row
coverage, not reduced network traffic or measured latency. Sorting an already
capped result cannot recover omitted items.

The second notebook exercises all four released COPY handlers. `feed.auto`
imports a local file; `feed.rss`, `feed.atom` and `feed.jsonfeed` export rows ordered
by an explicit SQL column. Output files are confined to `artifacts/roundtrip` and
replaced on rerun. Paths are on the worker host; the attached release is a local
subprocess here. Nothing is published to an external feed.

The round-trip contract checks ten unique absolute URN IDs, titles, links,
publication instants and category sets. A full join catches unexpected as well as
missing IDs. Re-imported `seq` is a document position, not stable identity. Atom
fills absent update times from the known publication times in this fixture;
those values are not observations of later publisher edits. RSS and JSON retain
the absent update field. These checks establish neither byte equality nor lossless
preservation of all format-specific fields or strict feed-standard validation.

## Run the experiments separately

Double the retrieval cap while leaving the documents and time window fixed:

```sh
uv run --locked vgi-notebooks 01_measure_feed_prefix_coverage.ipynb \
  --parameters '{"MAX_ITEMS":8}' --output-dir artifacts/cap-eight
```

Recent recall rises from 2/6 to 5/6, while full-feed metadata and normalized
common fields stay fixed. The accepted range is integer 1–10. Restore `4`.

Export newest publication first without changing the population:

```sh
uv run --locked vgi-notebooks 02_export_order_is_not_identity.ipynb \
  --parameters '{"EXPORT_ORDER":"published_desc"}' --output-dir artifacts/newest-first
```

IDs, common fields and timestamp-fallback counts stay fixed; source positions
change. The accepted values are `"source"` and `"published_desc"`. Restore
`"source"` for the default.

The shared runner requires separate output directories for parameter overrides
and writes a notebook only after success. Defaults, both experiments, all changed
charts and one fresh preloaded-kernel bootstrap are checked. Hosted Colab was not
manually operated. Review PNGs, generated feed files and experiment copies are
ignored; native SVG outputs and GitHub SVG assets are committed.

[Coverage evidence](coverage.json) retains all seven logical SQL surfaces,
including the view and all COPY handlers. Actual release execution is separate
from current-source compatibility. Current changes add examples/metadata; parsing,
transport, encoders, signatures and root dependencies are unchanged. Existing green
source CI is reused for these notebook-only updates.
