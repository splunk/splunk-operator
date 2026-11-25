# Run inside the Linux fixture pod. Observe the same boundary as the supervisor.
import ctypes
import os
from pathlib import Path
import select
import struct
import sys
import time


IN_MOVED_TO = 0x80
EVENT = struct.Struct("iIII")  # struct inotify_event, before the variable-length name
ROOT = Path("/opt/splunk/var/run/supervisor/postgresok")
DATA_LINK = ROOT / "..data"
ENDPOINT = ROOT / "connection/CLUSTER_RW_ENDPOINT"


def watch(initial_value, updated_value, ready_file, timeout):
    libc = ctypes.CDLL("libc.so.6", use_errno=True)
    fd = libc.inotify_init1(os.O_CLOEXEC)
    if fd < 0:
        raise OSError(ctypes.get_errno(), "inotify_init1 failed")

    try:
        watch_id = libc.inotify_add_watch(fd, os.fsencode(ROOT), IN_MOVED_TO)
        if watch_id < 0:
            raise OSError(ctypes.get_errno(), "inotify_add_watch failed")
        initial_target = os.readlink(DATA_LINK)
        if ENDPOINT.read_text() != initial_value:
            raise RuntimeError("Unexpected initial value in the mounted endpoint file")
        # The pod's readiness probe signals that the watch is armed.
        deadline = time.monotonic() + timeout
        Path(ready_file).touch()
        while (remaining := deadline - time.monotonic()) > 0:
            if not select.select([fd], [], [], remaining)[0]:
                break
            events = os.read(fd, 4096)
            offset = 0
            while offset < len(events):
                wd, mask, _, length = EVENT.unpack_from(events, offset)
                offset += EVENT.size
                name = events[offset:offset + length].split(b"\0", 1)[0]
                offset += length
                if wd == watch_id and mask & IN_MOVED_TO and name == b"..data":
                    if os.readlink(DATA_LINK) != initial_target and ENDPOINT.read_text() == updated_value:
                        return
        raise TimeoutError("No IN_MOVED_TO event for ..data exposed the updated endpoint")
    finally:
        os.close(fd)


if __name__ == "__main__":
    before, after, marker, seconds = sys.argv[1:]
    watch(before, after, marker, int(seconds))
