from __future__ import annotations

import os
import pty
import select
import subprocess
import sys
import termios
import tty
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class RunResult:
    returncode: int
    stdout: str = ""
    stderr: str = ""


class SubprocessRunner:
    def run(self, argv: list[str], *, cwd: Path | None = None, capture: bool = False) -> RunResult:
        if capture:
            proc = subprocess.run(argv, cwd=cwd, text=True, capture_output=True)
            return RunResult(returncode=proc.returncode, stdout=proc.stdout, stderr=proc.stderr)

        proc = subprocess.run(argv, cwd=cwd)
        return RunResult(returncode=proc.returncode)

    def run_interactive(self, argv: list[str], *, cwd: Path | None = None) -> RunResult:
        stdin_fd = sys.stdin.fileno()
        stdout_fd = sys.stdout.fileno()

        if not os.isatty(stdin_fd) or not os.isatty(stdout_fd):
            return RunResult(returncode=1, stderr="interactive mode requires a terminal\n")

        master_fd, slave_fd = pty.openpty()
        proc = subprocess.Popen(argv, cwd=cwd, stdin=slave_fd, stdout=slave_fd, stderr=slave_fd)
        os.close(slave_fd)

        old_tty = termios.tcgetattr(stdin_fd)
        tty.setraw(stdin_fd)

        try:
            while True:
                if proc.poll() is not None:
                    try:
                        data = os.read(master_fd, 65536)
                        if data:
                            os.write(stdout_fd, data)
                    except OSError:
                        pass
                    break

                readable, _, _ = select.select([master_fd, stdin_fd], [], [], 0.05)

                if master_fd in readable:
                    try:
                        data = os.read(master_fd, 65536)
                    except OSError:
                        data = b""
                    if data:
                        os.write(stdout_fd, data)

                if stdin_fd in readable:
                    try:
                        incoming = os.read(stdin_fd, 65536)
                    except OSError:
                        incoming = b""
                    if incoming:
                        try:
                            os.write(master_fd, incoming)
                        except OSError:
                            pass
        finally:
            termios.tcsetattr(stdin_fd, termios.TCSADRAIN, old_tty)
            os.close(master_fd)

        return RunResult(returncode=proc.wait())
