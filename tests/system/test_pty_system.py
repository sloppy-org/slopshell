from __future__ import annotations

import asyncio
import os
from pathlib import Path

from tabula.web.pty import LocalPtyTransport

from .conftest import read_pty_until


def test_pty_open_and_echo(tmp_path: Path) -> None:
    async def _run() -> None:
        transport = await LocalPtyTransport.open(str(tmp_path))
        try:
            transport.write(b"echo PTY_ECHO_TEST_XYZ\n")
            output = await read_pty_until(transport._fd, b"PTY_ECHO_TEST_XYZ")
            assert b"PTY_ECHO_TEST_XYZ" in output
        finally:
            transport.close()

    asyncio.run(_run())


def test_pty_cwd(tmp_path: Path) -> None:
    async def _run() -> None:
        transport = await LocalPtyTransport.open(str(tmp_path))
        try:
            transport.write(b"pwd\n")
            output = await read_pty_until(transport._fd, str(tmp_path).encode())
            assert str(tmp_path).encode() in output
        finally:
            transport.close()

    asyncio.run(_run())


def test_pty_nonexistent_cwd() -> None:
    async def _run() -> None:
        try:
            transport = await LocalPtyTransport.open("/nonexistent/path/xyz")
            transport.close()
            raise AssertionError("expected an error for nonexistent cwd")
        except (FileNotFoundError, OSError):
            pass

    asyncio.run(_run())


def test_pty_session_leader(tmp_path: Path) -> None:
    """Verify os.forkpty() makes the child a session and process group leader."""

    async def _run() -> None:
        transport = await LocalPtyTransport.open(str(tmp_path))
        try:
            pid = transport._pid
            pgid = os.getpgid(pid)
            sid = os.getsid(pid)
            assert pgid == pid
            assert sid == pid
        finally:
            transport.close()

    asyncio.run(_run())


def test_pty_resize(tmp_path: Path) -> None:
    async def _run() -> None:
        transport = await LocalPtyTransport.open(str(tmp_path))
        try:
            transport.resize(132, 50)
            transport.write(b"stty size\n")
            output = await read_pty_until(transport._fd, b"50 132")
            assert b"50 132" in output
        finally:
            transport.close()

    asyncio.run(_run())


def test_pty_close_cleanup(tmp_path: Path) -> None:
    async def _run() -> None:
        transport = await LocalPtyTransport.open(str(tmp_path))
        pid = transport._pid
        fd = transport._fd
        transport.close()

        for _ in range(40):
            try:
                reaped, _ = os.waitpid(pid, os.WNOHANG)
                if reaped == pid:
                    break
            except ChildProcessError:
                break
            await asyncio.sleep(0.05)
        else:
            raise AssertionError("process should be terminated after close")

        try:
            os.fstat(fd)
            fd_open = True
        except OSError:
            fd_open = False
        assert not fd_open, "fd should be closed"

    asyncio.run(_run())


def test_pty_concurrent_sessions(tmp_path: Path) -> None:
    d1 = tmp_path / "session1"
    d1.mkdir()
    d2 = tmp_path / "session2"
    d2.mkdir()

    async def _run() -> None:
        t1 = await LocalPtyTransport.open(str(d1))
        t2 = await LocalPtyTransport.open(str(d2))
        try:
            t1.write(b"echo SESSION_ONE_MARKER\n")
            t2.write(b"echo SESSION_TWO_MARKER\n")

            out1 = await read_pty_until(t1._fd, b"SESSION_ONE_MARKER")
            out2 = await read_pty_until(t2._fd, b"SESSION_TWO_MARKER")

            assert b"SESSION_ONE_MARKER" in out1
            assert b"SESSION_TWO_MARKER" in out2
            assert b"SESSION_TWO_MARKER" not in out1
            assert b"SESSION_ONE_MARKER" not in out2
        finally:
            t1.close()
            t2.close()

    asyncio.run(_run())
