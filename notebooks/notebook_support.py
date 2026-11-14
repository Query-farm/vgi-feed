"""Serve explicit synthetic inputs; parsing and analytical calculations remain in SQL."""

from __future__ import annotations

from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from threading import Thread

import matplotlib.pyplot as plt
import seaborn as sns
from IPython.display import Markdown, display


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


def prose(text: str) -> None:
    display(Markdown(text))


def theme() -> None:
    sns.set_theme(
        style="whitegrid",
        rc={
            "figure.dpi": 145,
            "axes.spines.top": False,
            "axes.spines.right": False,
            "grid.color": "#E8EDE9",
            "text.color": "#243B2F",
            "axes.labelcolor": "#405047",
            "text.parse_math": False,
            "legend.frameon": False,
        },
    )


def chart(fig, title: str, subtitle: str, source: str) -> None:
    fig.text(0.08, 0.98, title, ha="left", va="top", fontsize=17, fontweight="semibold")
    fig.text(0.08, 0.90, subtitle, ha="left", va="top", fontsize=10, color="#58685F")
    fig.tight_layout(rect=(0.02, 0.11, 1, 0.82))
    fig.text(0.08, 0.03, source, fontsize=8, color="#6C7D73")
    plt.show()
