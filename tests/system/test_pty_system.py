from __future__ import annotations

import asyncio
import os
from pathlib import Path
from unittest.mock import patch

from tabula.web.pty import LocalPtyTransport


async def _read_until(fd: int, marker: bytes, timeout: float = 5.0) -> bytes:
    output = b""
    deadline = asyncio.get_event_loop().time() + timeout
    while asyncio.get_event_loop().time() < deadline:
        try:
            chunk = os.read(fd, 4096)
            output += chunk
            if marker in output:
                return output
        except BlockingIOError:
            await asyncio.sleep(0.05)
    return output


def test_pty_open_and_echo(tmp_path: Path) -> None:
    async def _run() -> None:
        transport = await LocalPtyTransport.open(str(tmp_path))
        try:
            transport.write(b"echo PTY_ECHO_TEST_XYZ\n")
            output = await _read_until(transport._fd, b"PTY_ECHO_TEST_XYZ")
            assert b"PTY_ECHO_TEST_XYZ" in output
        finally:
            transport.close()

    asyncio.run(_run())


def test_pty_cwd(tmp_path: Path) -> None:
    async def _run() -> None:
        transport = await LocalPtyTransport.open(str(tmp_path))
        try:
            transport.write(b"pwd\n")
            output = await _read_until(transport._fd, str(tmp_path).encode())
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


def test_pty_process_group(tmp_path: Path) -> None:
    """Verify process_group=0 puts the child in its own group.

    On systems where process_group=0 falls back (containers, restricted
    envs), the child inherits the parent's pgid and this assertion won't
    hold -- the fallback path is tested separately in
    test_pty_process_group_fallback.
    """

    async def _run() -> None:
        transport = await LocalPtyTransport.open(str(tmp_path))
        try:
            pid = transport._process.pid
            pgid = os.getpgid(pid)
            assert pgid == pid
        finally:
            transport.close()

    asyncio.run(_run())


def test_pty_process_group_fallback(tmp_path: Path) -> None:
    call_count = 0
    original = asyncio.create_subprocess_exec

    async def _mock_exec(*args, **kwargs):
        nonlocal call_count
        call_count += 1
        if "process_group" in kwargs:
            raise PermissionError("process_group not allowed")
        return await original(*args, **kwargs)

    async def _run() -> None:
        with patch("tabula.web.pty.asyncio.create_subprocess_exec", side_effect=_mock_exec):
            transport = await LocalPtyTransport.open(str(tmp_path))
            try:
                assert call_count == 2
                transport.write(b"echo FALLBACK_OK\n")
                output = await _read_until(transport._fd, b"FALLBACK_OK")
                assert b"FALLBACK_OK" in output
            finally:
                transport.close()

    asyncio.run(_run())


def test_pty_resize(tmp_path: Path) -> None:
    async def _run() -> None:
        transport = await LocalPtyTransport.open(str(tmp_path))
        try:
            transport.resize(132, 50)
            transport.write(b"stty size\n")
            output = await _read_until(transport._fd, b"50 132")
            assert b"50 132" in output
        finally:
            transport.close()

    asyncio.run(_run())


def test_pty_close_cleanup(tmp_path: Path) -> None:
    async def _run() -> None:
        transport = await LocalPtyTransport.open(str(tmp_path))
        pid = transport._process.pid
        fd = transport._fd
        transport.close()

        for _ in range(40):
            try:
                os.kill(pid, 0)
            except ProcessLookupError:
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

            out1 = await _read_until(t1._fd, b"SESSION_ONE_MARKER")
            out2 = await _read_until(t2._fd, b"SESSION_TWO_MARKER")

            assert b"SESSION_ONE_MARKER" in out1
            assert b"SESSION_TWO_MARKER" in out2
            assert b"SESSION_TWO_MARKER" not in out1
            assert b"SESSION_ONE_MARKER" not in out2
        finally:
            t1.close()
            t2.close()

    asyncio.run(_run())
