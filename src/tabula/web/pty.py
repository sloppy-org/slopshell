from __future__ import annotations

import asyncio
import fcntl
import logging
import os
import signal
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
    """PTY transport using a local subprocess with proper session control."""

    def __init__(self, master_fd: int, pid: int) -> None:
        self._fd = master_fd
        self._pid = pid

    @classmethod
    async def open(cls, cwd: str) -> LocalPtyTransport:
        if not os.path.isdir(cwd):
            raise FileNotFoundError(f"No such directory: {cwd}")
        pid, master_fd = os.forkpty()
        if pid == 0:
            try:
                os.chdir(cwd)
                shell = os.environ.get("SHELL", "/bin/bash")
                os.execvp(shell, ["-" + os.path.basename(shell)])
            except BaseException:
                os._exit(1)
        os.set_blocking(master_fd, False)
        return cls(master_fd, pid)

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
            os.kill(self._pid, signal.SIGTERM)
        except ProcessLookupError:
            return
        try:
            reaped, _ = os.waitpid(self._pid, os.WNOHANG)
            if reaped:
                return
        except ChildProcessError:
            return
        pid = self._pid
        try:
            loop = asyncio.get_running_loop()
            loop.run_in_executor(None, os.waitpid, pid, 0)
        except RuntimeError:
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
