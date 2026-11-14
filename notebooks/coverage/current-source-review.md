# Feed catalog review

Actual public release v0.5.0 exposes feed_info, feed_items, read_feed_copy,
write_atom_copy, write_jsonfeed_copy, write_rss_copy and the feed_registry view.
Complete functions/tables/views exports and actual vgi_copy_formats registration
rows are included. COPY handlers count by their public handler name; option rows
and the format aliases do not multiply the denominator. All four handlers remain
included, with write access recorded for the three writers. There are no secret
providers or omitted authenticated objects.

Current source21c235e6 has the same registrations, argument/return schemas and
COPY option signatures. The full release-to-current runtime/dependency diff
changes only functions.go example constants, metadata descriptions and agent tasks.
No parsing, fetch, read/write processing, encoder or dependency changes occur.
The current binary was not separately built or executed. Current coverage means
reviewed source compatibility; saved notebook receipts prove onlyv0.5.0 execution.
Existing green source CI43571121132 at21c235e6 is reused for notebook-only work.
