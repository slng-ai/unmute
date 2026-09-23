"""Child processes this tool starts and stops: workers, bots and tunnels."""

import os
import re
import signal
import subprocess
import time
import urllib.error
import urllib.request
from collections.abc import Callable
from pathlib import Path
from types import TracebackType
from typing import Self


class NotReady(RuntimeError):
    """A process exited, or did not become ready in time."""


class Process:
    """A child process with its output in a log file.

    It runs in its own session, so stop() ends it and every child it started.
    uv, for one, starts the Python process as a child of its own.

    Args:
        name: What the process is, for error messages.
        cmd: The command line.
        cwd: The working directory.
        env: The whole environment.
        log: Where stdout and stderr go.
    """

    def __init__(self, name: str, cmd: list[str], cwd: Path, env: dict[str, str], log: Path) -> None:
        self.name = name
        self.log = log
        with log.open("w") as out:
            self._proc = subprocess.Popen(
                cmd, cwd=cwd, env=env, stdout=out, stderr=subprocess.STDOUT, start_new_session=True
            )

    def output(self) -> str:
        """Everything the process has written so far."""
        return self.log.read_text(errors="replace")

    def wait_until(self, ready: Callable[[], bool], timeout_s: float) -> None:
        """Block until ready() is true.

        A process that never gets ready is stopped before this raises, so the
        caller has nothing left to clean up.

        Raises:
            NotReady: If the process exits first, or timeout_s passes.
        """
        deadline = time.monotonic() + timeout_s
        while time.monotonic() < deadline:
            if self._proc.poll() is not None:
                raise NotReady(f"{self.name} exited; see {self.log}")
            if ready():
                return
            time.sleep(1)
        self.stop()
        raise NotReady(f"{self.name} was not ready after {timeout_s:.0f}s; see {self.log}")

    def stop(self) -> None:
        """End the process and its children, forcefully after 20 seconds."""
        if self._proc.poll() is not None:
            return
        os.killpg(self._proc.pid, signal.SIGTERM)
        try:
            self._proc.wait(20)
        except subprocess.TimeoutExpired:
            os.killpg(self._proc.pid, signal.SIGKILL)

    def __enter__(self) -> Self:
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        self.stop()


def answers_http(url: str) -> bool:
    """Whether anything answers HTTP at url. An error status still counts."""
    try:
        urllib.request.urlopen(url, timeout=5)
    except urllib.error.HTTPError:
        return True
    except OSError:
        return False
    return True


class Tunnel(Process):
    """A cloudflared quick tunnel to a local port: no account, a new host each time.

    Args:
        port: The local port to expose.
        workdir: Where the log goes.
    """

    def __init__(self, port: int, workdir: Path) -> None:
        super().__init__(
            f"cloudflared on port {port}",
            ["cloudflared", "tunnel", "--no-autoupdate", "--url", f"http://127.0.0.1:{port}"],
            workdir,
            dict(os.environ),
            workdir / f"tunnel-{port}.log",
        )
        # Ready means cloudflared says the edge accepted the connection, which
        # can come twenty seconds after it prints the host. Probing the public
        # URL from here instead does not work: a lookup made before the host
        # exists is cached as missing, and the probe keeps failing long after
        # Coval could reach it.
        self.wait_until(lambda: "Registered tunnel connection" in self.output(), 120)
        found = re.search(r"https://([a-z0-9-]+\.trycloudflare\.com)", self.output())
        if not found:
            self.stop()
            raise NotReady(f"{self.name} printed no host; see {self.log}")
        self.host = found.group(1)
