"""`coval-sim`: let Coval call the examples, running on this laptop.

    coval-sim run                      # every test set attached to an unmute-* agent
    coval-sim run KG5MJ39q abc12345    # only these test sets
    coval-sim run KG5MJ39q --target livekit --iterations 3
    coval-sim add customer-intake      # create that example's two Coval agents

Everything about what to test lives in Coval. A test set is tested against every
agent named unmute-<example>-<target> it is attached to, with that agent's
metrics. So to cover an example, attach a test set to its agents in Coval.
"""

import argparse
import os
import secrets
import subprocess
import sys
import tempfile
from collections import defaultdict
from contextlib import ExitStack
from dataclasses import dataclass, field
from pathlib import Path

from coval_sim.coval import CovalClient, CovalError
from coval_sim.livekit import TokenServer
from coval_sim.models import Agent, Persona
from coval_sim.runtime import NotReady, Tunnel
from coval_sim.targets import TARGETS, Endpoints, Example, read_env

TOKEN_PORT = 8089
RUN_TIMEOUT_S = 30 * 60


@dataclass(frozen=True)
class Job:
    """One run to launch: a test set against one agent."""

    agent: Agent
    test_set_id: str


@dataclass
class Row:
    """One line of the report: what ran, and why it failed if it did."""

    example: str
    target: str
    test_set_id: str = "-"
    run_id: str = "-"
    problems: list[str] = field(default_factory=list)
    log: Path | None = None

    def render(self) -> str:
        """The row, then each problem indented under it."""
        status = "FAIL" if self.problems else "ok  "
        lines = [f"{status}  {self.example}  {self.target}  test set {self.test_set_id}  run {self.run_id}"]
        lines += [f"      {problem}" for problem in self.problems]
        if self.problems and self.log:
            lines.append(f"      log: {self.log}")
        return "\n".join(lines)


class Repository:
    """The unmute checkout this tool runs in.

    Args:
        root: The directory holding go.mod and examples/.
    """

    def __init__(self, root: Path) -> None:
        self.root = root

    @classmethod
    def find(cls, start: Path) -> "Repository":
        """The nearest checkout at or above start.

        Raises:
            FileNotFoundError: If no parent holds go.mod and examples/.
        """
        for directory in (start, *start.parents):
            if (directory / "go.mod").exists() and (directory / "examples").is_dir():
                return cls(directory)
        raise FileNotFoundError(f"no unmute checkout at or above {start}")

    def env(self) -> dict[str, str]:
        """The repository-root .env, with the real environment on top."""
        return {**read_env(self.root / ".env"), **os.environ}

    def example(self, name: str) -> Example:
        """The package examples/<name>."""
        return Example(self.root / "examples" / name)

    def build_unmute(self, workdir: Path) -> Path:
        """Build unmute from this checkout, so a stale bin/unmute cannot lie."""
        binary = workdir / "unmute"
        subprocess.run(["go", "build", "-o", str(binary), "."], cwd=self.root, check=True)
        return binary


class ReleaseCheck:
    """Plans and runs Coval simulations against the examples.

    Args:
        repo: The checkout holding the examples.
        coval: The Coval client.
        workdir: Where scratch copies, binaries and logs go.
    """

    def __init__(self, repo: Repository, coval: CovalClient, workdir: Path) -> None:
        self.repo = repo
        self.coval = coval
        self.workdir = workdir

    def plan(self, test_set_ids: list[str], targets: list[str]) -> tuple[dict[str, list[Job]], list[Row]]:
        """Which runs to launch, grouped by example, from what Coval holds.

        Args:
            test_set_ids: The test sets to run. Empty means every test set
                attached to one of our agents.
            targets: The target kinds to include.

        Returns:
            The jobs per example, and a failed row for each test set nothing runs.
        """
        jobs: dict[str, list[Job]] = defaultdict(list)
        for agent in self.coval.agents():
            if agent.example is None or agent.target not in targets:
                continue
            wanted = [t for t in agent.test_set_ids if not test_set_ids or t in test_set_ids]
            jobs[agent.example] += [Job(agent, test_set_id) for test_set_id in wanted]
        covered = {job.test_set_id for example_jobs in jobs.values() for job in example_jobs}
        missing = [
            Row("-", "-", test_set_id, problems=["no unmute-<example>-<target> agent has this test set attached"])
            for test_set_id in test_set_ids
            if test_set_id not in covered
        ]
        return {example: j for example, j in jobs.items() if j}, missing

    def run(self, plan: dict[str, list[Job]], persona: Persona, iterations: int, concurrency: int) -> list[Row]:
        """Start what the plan needs, run every job, and report one row per job."""
        kinds = {job.agent.target for jobs in plan.values() for job in jobs}
        env = self.repo.env()
        unmute = self.repo.build_unmute(self.workdir)
        rows = []
        with ExitStack() as stack:
            endpoints = self._endpoints(stack, env, kinds)
            for example, jobs in sorted(plan.items()):
                try:
                    rows += self._check(example, jobs, endpoints, env, unmute, persona, iterations, concurrency)
                except (NotReady, RuntimeError, ValueError) as error:
                    rows.append(Row(example, "-", problems=[str(error)]))
        return rows

    def _endpoints(self, stack: ExitStack, env: dict[str, str], kinds: set[str | None]) -> Endpoints:
        auth = secrets.token_urlsafe(24)
        token_host = pipecat_host = ""
        if "livekit" in kinds:
            stack.enter_context(
                TokenServer(
                    env["LIVEKIT_URL"],
                    env["LIVEKIT_API_KEY"],
                    env["LIVEKIT_API_SECRET"],
                    auth,
                    self.workdir / "token-server.log",
                    TOKEN_PORT,
                )
            )
            token_host = stack.enter_context(Tunnel(TOKEN_PORT, self.workdir)).host
        pipecat_port = Endpoints.pipecat_port
        if "pipecat" in kinds:
            pipecat_host = stack.enter_context(Tunnel(pipecat_port, self.workdir)).host
        return Endpoints(env.get("LIVEKIT_URL", ""), token_host, pipecat_host, pipecat_port, auth)

    def _check(
        self,
        example_name: str,
        jobs: list[Job],
        endpoints: Endpoints,
        env: dict[str, str],
        unmute: Path,
        persona: Persona,
        iterations: int,
        concurrency: int,
    ) -> list[Row]:
        example = self.repo.example(example_name)
        compiled = example.compile(unmute, self.workdir)
        launched: list[tuple[Job, str, Path]] = []
        rows = []
        with ExitStack() as stack:
            agents = {job.agent.id: job.agent for job in jobs}
            for agent in agents.values():
                target = compiled.get(agent.target or "")
                if target is None:
                    rows.append(Row(example_name, agent.target or "-", problems=[
                        f"Coval agent {agent.display_name} exists, but {example_name} has no {agent.target} target"
                    ]))
                    continue
                log = self.workdir / f"{example_name}-{target.kind}.log"
                stack.enter_context(target.start(example.env(env), endpoints, log))
                self.coval.update_connection(agent, target.connection(endpoints))
                for job in (j for j in jobs if j.agent.id == agent.id):
                    label = f"release-check {example_name} {target.kind}"
                    run_id = self.coval.launch(
                        agent, persona, job.test_set_id, iterations=iterations, concurrency=concurrency, label=label
                    )
                    launched.append((job, run_id, log))
            for job, run_id, log in launched:
                run = self.coval.wait(run_id, timeout_s=RUN_TIMEOUT_S)
                problems = (
                    run.problems(self.coval.simulations(run_id))
                    if run.finished
                    else [f"run still {run.status} after {RUN_TIMEOUT_S}s"]
                )
                rows.append(Row(example_name, job.agent.target or "-", job.test_set_id, run_id, problems, log))
        return rows

    def add(self, example_name: str, targets: list[str]) -> list[str]:
        """Create the Coval agents for an example, with placeholder connections.

        The connection is replaced on every run, because each run gets new tunnel
        hosts, so the placeholders never reach a call.

        Returns:
            One line per agent: created, or already there.
        """
        example = self.repo.example(example_name)
        compiled = example.compile(self.repo.build_unmute(self.workdir), self.workdir)
        existing = {agent.display_name for agent in self.coval.agents()}
        placeholder = Endpoints(
            livekit_url=self.repo.env().get("LIVEKIT_URL", "wss://example.invalid"),
            token_host="example.invalid",
            pipecat_host="example.invalid",
        )
        lines = []
        for kind in targets:
            target = compiled.get(kind)
            name = Agent.name_for(example_name, kind)
            if target is None:
                lines.append(f"skipped {name}: {example_name} has no {kind} target")
            elif name in existing:
                lines.append(f"exists  {name}")
            else:
                self.coval.create_agent(name, target.coval_type, target.connection(placeholder))
                lines.append(f"created {name}")
        return lines


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="coval-sim", description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    commands = parser.add_subparsers(dest="command", required=True)
    run = commands.add_parser("run", help="run test sets against the examples")
    run.add_argument("test_set_ids", nargs="*", metavar="TEST_SET", help="Coval test set IDs; default is all")
    run.add_argument("--persona", default="Standard Customer", help="the Coval persona that places every call")
    run.add_argument("--iterations", type=int, default=1, help="times to run each test case")
    run.add_argument("--concurrency", type=int, default=3, help="calls in flight per run")
    add = commands.add_parser("add", help="create an example's Coval agents")
    add.add_argument("example", help="a directory name under examples/")
    for command in (run, add):
        command.add_argument("--target", choices=sorted(TARGETS), action="append", help="default: both")
    return parser


def _required(env: dict[str, str], needs_livekit: bool) -> list[str]:
    names = ["COVAL_API_KEY"] + (["LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"] if needs_livekit else [])
    return [name for name in names if not env.get(name)]


def main(argv: list[str] | None = None) -> int:
    """Run the command line. Returns the exit code: 0 passed, 1 failed."""
    args = _parser().parse_args(argv)
    targets = args.target or sorted(TARGETS)
    try:
        repo = Repository.find(Path.cwd())
    except FileNotFoundError as error:
        print(f"error: {error}", file=sys.stderr)
        return 1
    env = repo.env()
    missing = _required(env, args.command == "run" and "livekit" in targets)
    if missing:
        print(f"error: set {', '.join(missing)} in the environment or .env", file=sys.stderr)
        return 1
    check = ReleaseCheck(repo, CovalClient(env["COVAL_API_KEY"]), Path(tempfile.mkdtemp(prefix="coval-sim-")))
    try:
        if args.command == "add":
            lines = check.add(args.example, targets)
            print("\n".join(lines))
            if any(line.startswith("created") for line in lines):
                print("next: attach a test set to these agents in Coval, then `make sim TEST_SET=<its id>`")
            return 0
        plan, rows = check.plan(args.test_set_ids, targets)
        if not plan and not rows:
            print("error: no unmute-<example>-<target> agent has a test set attached", file=sys.stderr)
            return 1
        rows += check.run(plan, check.coval.persona(args.persona), args.iterations, args.concurrency)
    except (CovalError, ValueError, RuntimeError) as error:
        print(f"error: {error}", file=sys.stderr)
        return 1
    for row in rows:
        print(row.render())
    return 1 if any(row.problems for row in rows) else 0


if __name__ == "__main__":
    sys.exit(main())
