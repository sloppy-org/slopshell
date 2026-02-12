from __future__ import annotations

import asyncio
import fcntl
import logging
import os
import pty as pty_module
import struct
import termios
from abc import ABC, abstractmethod
from typing import Any

from aiohttp import web

_log = logging.getLogger(__name__)


class PtyTransport(ABC):
    """Abstract PTY transport for terminal WebSocket sessions."""

    @abstractmethod
    def write(self, data: bytes) -> None: ...

    @abstractmethod
    def resize(self, cols: int, rows: int) -> None: ...

    @abstractmethod
    def close(self) -> None: ...

    @abstractmethod
    async def reader(self, ws: web.WebSocketResponse) -> None: ...


class LocalPtyTransport(PtyTransport):
    """PTY transport using a local subprocess."""

    def __init__(self, master_fd: int, process: asyncio.subprocess.Process) -> None:
        self._fd = master_fd
        self._process = process

    @classmethod
    async def open(cls, cwd: str) -> LocalPtyTransport:
        master_fd, slave_fd = pty_module.openpty()
        shell = os.environ.get("SHELL", "/bin/bash")
        try:
            try:
                process = await asyncio.create_subprocess_exec(
                    shell, stdin=slave_fd, stdout=slave_fd, stderr=slave_fd,
                    process_group=0, cwd=cwd,
                )
            except (PermissionError, OSError) as exc:
                _log.warning("process_group=0 failed (%s), retrying without", exc)
                process = await asyncio.create_subprocess_exec(
                    shell, stdin=slave_fd, stdout=slave_fd, stderr=slave_fd,
                    cwd=cwd,
                )
        except BaseException:
            os.close(slave_fd)
            os.close(master_fd)
            raise
        os.close(slave_fd)
        os.set_blocking(master_fd, False)
        return cls(master_fd, process)

    def write(self, data: bytes) -> None:
        os.write(self._fd, data)

    def resize(self, cols: int, rows: int) -> None:
        fcntl.ioctl(self._fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))

    def close(self) -> None:
        try:
            os.close(self._fd)
        except OSError:
            pass
        try:
            self._process.terminate()
        except ProcessLookupError:
            pass

    async def reader(self, ws: web.WebSocketResponse) -> None:
        loop = asyncio.get_running_loop()
        fd = self._fd
        queue: asyncio.Queue[bytes | None] = asyncio.Queue()

        def _on_readable() -> None:
            try:
                data = os.read(fd, 4096)
                queue.put_nowait(data if data else None)
            except OSError:
                queue.put_nowait(None)

        loop.add_reader(fd, _on_readable)
        try:
            while True:
                data = await queue.get()
                if data is None:
                    break
                await ws.send_bytes(data)
        except (asyncio.CancelledError, ConnectionResetError):
            pass
        finally:
            try:
                loop.remove_reader(fd)
            except (ValueError, OSError):
                pass


class SshPtyTransport(PtyTransport):
    """PTY transport over SSH using asyncssh."""

    def __init__(self, process: Any) -> None:
        self._process = process

    def write(self, data: bytes) -> None:
        self._process.stdin.write(data)

    def resize(self, cols: int, rows: int) -> None:
        self._process.change_terminal_size(cols, rows)

    def close(self) -> None:
        self._process.close()

    async def reader(self, ws: web.WebSocketResponse) -> None:
        try:
            while not self._process.stdout.at_eof():
                data = await self._process.stdout.read(4096)
                if not data:
                    break
                if isinstance(data, bytes):
                    await ws.send_bytes(data)
                else:
                    await ws.send_str(data)
        except (asyncio.CancelledError, ConnectionResetError):
            pass
