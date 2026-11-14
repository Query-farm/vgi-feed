# Synthetic source-order fixtures

These three documents were authored for this notebook, not downloaded from a publisher. They encode the same ten IDs, titles, links, publication instants and category lists in the same deliberately nonchronological order. All times are UTC; `https://example.invalid` links identify fictional entries and are never fetched. The local fixture server returns these bytes unchanged to the actual released Go worker.

The fixed analysis window is [2026-01-13 12:00, 2026-01-15 12:00) UTC. It includes the lower boundary and excludes the anchor. One entry is exactly at the excluded anchor, one is four hours later, and two are older than the window. All ten publication dates are parseable. Unknown dates are still handled separately by the SQL diagnostics. Category memberships overlap; category counts must not be added as if they partition entries.

RSS uses RFC 2822 dates; Atom and JSON Feed use RFC 3339. Atom also includes `updated` equal to `published` for each entry. Only common normalized fields are compared: the worker does not promise lossless equality of every format-specific field. This is a parser and query-order investigation, not a measurement of any real publication's freshness or a strict feed-validator test.
