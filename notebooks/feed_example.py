"""Serve explicit synthetic inputs; parsing and analytical calculations remain in SQL."""

from __future__ import annotations

from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from threading import Thread

import matplotlib.pyplot as plt
import numpy as np
from vgi_notebooks import finish


def configure(cap):
    if type(cap) is not int or not 1 <= cap <= 10:
        raise ValueError("MAX_ITEMS must be an integer from 1 through 10")
    directory = Path("fixtures").resolve()
    requests = []
    names = {
        "/feed.rss": "application/rss+xml",
        "/feed.atom": "application/atom+xml",
        "/feed.json": "application/feed+json",
    }

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            requests.append(self.path)
            if self.path not in names:
                self.send_error(404)
                return
            payload = (directory / self.path.lstrip("/")).read_bytes()
            self.send_response(200)
            self.send_header("Content-Type", names[self.path])
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)

        def log_message(self, *args):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, thread, requests, f"http://127.0.0.1:{server.server_port}"


def verify_requests(requests):
    expected = {"/feed.rss": 3, "/feed.atom": 3, "/feed.json": 3}
    actual = {path: requests.count(path) for path in set(requests)}
    if actual != expected:
        raise AssertionError(f"Expected one info, full and capped fetch per format; got {actual}")
    return (
        "HTTP audit passed: nine real worker requests, three per synthetic format. Inline "
        "RSS parsing makes no HTTP request."
    )


def plot_positions(positions, cap):
    """Separate returned source positions from publication ages."""
    fig, ax = plt.subplots(figsize=(11.2, 6.5))
    ax.axvspan(0, 48, color="#E8F2E7", zorder=0)
    colors = {
        "Recent": "#247F70",
        "Before window": "#939B9A",
        "At/after anchor": "#C77842",
        "Unknown date": "#836CB1",
    }
    for row in positions.itertuples():
        if np.isfinite(row.age_hours):
            ax.scatter(
                row.age_hours,
                row.source_position,
                s=95,
                marker="o" if row.retrieved else "X",
                color=colors[row.time_class],
                edgecolor="white",
                linewidth=0.7,
                zorder=3,
            )
    ax.axhline(cap + 0.5, color="#536077", ls="--", lw=1.2)
    ax.set_yticks(positions.source_position)
    ax.invert_yaxis()
    ax.set(
        xlabel="Age at the fixed UTC anchor (hours; negative = future-dated)",
        ylabel="Source position",
        xlim=(min(-8, positions.age_hours.min() - 4), positions.age_hours.max() + 6),
    )
    ax.grid(axis="y", alpha=0.5)
    finish(
        fig,
        "A short source prefix can miss recent entries",
        f"Cap {cap} · circles are retrieved; × marks are omitted · dashed line ends the prefix",
        "Synthetic bulletin · released VGI Feed v0.5.0 · anchor 15 January 2026, 12:00 UTC\n"
        "Green band: 0 < age ≤ 48 hours. Age zero and future-dated entries are excluded.",
        name="01-source-order",
    )


def plot_categories(categories, cap):
    """Preserve overlapping category memberships and the full-feed denominator."""
    fig, ax = plt.subplots(figsize=(10.8, 5.8))
    y = np.arange(len(categories))
    ax.barh(y, categories.recent_retrieved, color="#247F70", label="Retrieved recent entries", height=0.48)
    ax.barh(
        y,
        categories.recent_missed,
        left=categories.recent_retrieved,
        color="#D5DFD8",
        label="Missed recent entries",
        height=0.48,
    )
    for i, row in enumerate(categories.itertuples()):
        ax.text(
            row.recent_total + 0.12,
            i,
            f"{row.recent_retrieved}/{row.recent_total} · {row.recall_pct:.0f}%",
            va="center",
            fontsize=11,
        )
    ax.set_yticks(y, categories.category)
    ax.invert_yaxis()
    ax.set(xlabel="Recent entries carrying each label (count)", xlim=(0, categories.recent_total.max() + 1.8))
    ax.xaxis.get_major_locator().set_params(integer=True)
    ax.grid(axis="x")
    fig.legend(
        *ax.get_legend_handles_labels(), loc="upper left", bbox_to_anchor=(0.07, 0.82), ncol=2, fontsize=9
    )
    finish(
        fig,
        "A source-order cap also changes topic coverage",
        f"Cap {cap} · labels show retrieved / complete recent entries and recall",
        "Synthetic bulletin · released VGI Feed v0.5.0 · fixed 48-hour UTC window\n"
        "Category labels overlap: a single entry can contribute to several bars.",
        name="02-topic-coverage",
    )


def validate_order(value):
    if value not in ("source", "published_desc"):
        raise ValueError("EXPORT_ORDER must be source or published_desc.")


def plot_roundtrip(positions, order):
    """Show stable IDs separately from regenerated source positions."""
    fig, ax = plt.subplots(figsize=(11, 7))
    y = np.arange(len(positions))
    for i, row in enumerate(positions.itertuples()):
        ax.plot(
            [row.source_position, row.export_position],
            [i - 0.12, i + 0.12],
            color="#CDD8D1",
            lw=1.5,
            zorder=1,
        )
    ax.scatter(
        positions.source_position, y - 0.12, color="#247F70", s=75, label="Original source position", zorder=3
    )
    ax.scatter(
        positions.export_position,
        y + 0.12,
        color="#C77842",
        s=65,
        marker="D",
        label="Re-imported position",
        zorder=3,
    )
    ax.set_yticks(y, [value.rsplit(":", 1)[-1] for value in positions.guid])
    ax.set_xticks(range(1, len(positions) + 1))
    ax.set(
        xlabel="Position within the document (one-based)",
        ylabel="Stable bulletin ID",
        xlim=(0.6, len(positions) + 0.4),
    )
    ax.invert_yaxis()
    ax.grid(axis="x")
    fig.legend(
        *ax.get_legend_handles_labels(), loc="upper left", bbox_to_anchor=(0.07, 0.82), ncol=2, fontsize=9
    )
    finish(
        fig,
        "Re-importing assigns positions from the exported order",
        f"Export order: {order} · all ten IDs and their common fields survive in every format",
        "Synthetic bulletin · actual released VGI Feed v0.5.0 COPY writers/readers\n"
        "Positions are shown one-based for reading; the worker's seq column is zero-based.",
        name="03-roundtrip-positions",
    )


def plot_updated(population):
    """Distinguish populated output dates from observed source updates."""
    fig, ax = plt.subplots(figsize=(10.8, 5.8))
    bars = ax.barh(
        population.format,
        population.updated_present,
        color=["#247F70" if v else "#A6B6AC" for v in population.updated_present],
        height=0.48,
    )
    ax.bar_label(bars, labels=[f"{v} of 10" for v in population.updated_present], padding=6, fontsize=12)
    ax.set(xlabel="Entries with a populated updated timestamp (count)", xlim=(0, 12))
    ax.xaxis.get_major_locator().set_params(integer=True)
    ax.invert_yaxis()
    ax.grid(axis="x")
    finish(
        fig,
        "A populated update field can be an export fallback",
        "Atom fills these missing updated values from published; the source has no observed update times.",
        "Synthetic bulletin · VGI Feed v0.5.0 · all ten publication timestamps are known\n"
        "The round-trip contract covers IDs, titles, links, publication dates and category sets, "
        "not all fields.",
        name="04-updated-fallback",
    )
