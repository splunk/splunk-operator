#!/usr/bin/env python3

"""Exercise HEC and distributed search through reusable HTTPS connections."""

from __future__ import annotations

import base64
import http.client
import json
import os
import re
import ssl
import sys
import time
import urllib.parse
from dataclasses import dataclass, field
from typing import Callable


RUN_ID_PATTERN = re.compile(r"^[A-Za-z0-9_.-]+$")


@dataclass
class ConnectionStats:
    opened: int = 0
    first_attempt_failures: int = 0
    recovered_requests: int = 0
    server_closes: int = 0
    max_requests_per_connection: int = 0
    response_versions: set[str] = field(default_factory=set)
    response_connection_headers: set[str] = field(default_factory=set)


class PersistentHTTPSClient:
    """Reuse one HTTPS connection and retry one interrupted logical request."""

    def __init__(
        self,
        host: str,
        port: int,
        timeout: int,
        connection_factory: Callable[..., http.client.HTTPSConnection]
        | None = None,
    ) -> None:
        context = ssl.create_default_context()
        context.check_hostname = False
        context.verify_mode = ssl.CERT_NONE
        self.host = host
        self.port = port
        self.timeout = timeout
        self.context = context
        self.connection_factory = connection_factory or http.client.HTTPSConnection
        self.connection: http.client.HTTPSConnection | None = None
        self.requests_on_connection = 0
        self.stats = ConnectionStats()

    def _connect(self) -> None:
        self.connection = self.connection_factory(
            self.host,
            self.port,
            timeout=self.timeout,
            context=self.context,
        )
        self.connection.connect()
        self.requests_on_connection = 0
        self.stats.opened += 1

    def _close(self) -> None:
        if self.connection is not None:
            self.connection.close()
        self.connection = None
        self.requests_on_connection = 0

    def request(
        self,
        method: str,
        path: str,
        body: bytes,
        headers: dict[str, str],
    ) -> tuple[int, bytes]:
        last_error: Exception | None = None
        for attempt in range(2):
            try:
                if self.connection is None or self.connection.sock is None:
                    self._connect()
                assert self.connection is not None
                self.connection.request(method, path, body=body, headers=headers)
                response = self.connection.getresponse()
                self.stats.response_versions.add(
                    {10: "HTTP/1.0", 11: "HTTP/1.1"}.get(
                        response.version,
                        f"HTTP/{response.version}",
                    )
                )
                connection_header = response.getheader("Connection")
                self.stats.response_connection_headers.add(
                    (connection_header or "Absent").strip().replace(" ", "_")
                )
                payload = response.read()
                self.requests_on_connection += 1
                self.stats.max_requests_per_connection = max(
                    self.stats.max_requests_per_connection,
                    self.requests_on_connection,
                )
                if response.will_close or self.connection.sock is None:
                    self.stats.server_closes += 1
                    self._close()
                if attempt == 1:
                    self.stats.recovered_requests += 1
                return response.status, payload
            except (OSError, http.client.HTTPException) as error:
                last_error = error
                if attempt == 0:
                    self.stats.first_attempt_failures += 1
                self._close()
        assert last_error is not None
        raise last_error

    def close(self) -> None:
        self._close()


def positive_integer(name: str, default: int) -> int:
    raw = os.environ.get(name, str(default))
    try:
        value = int(raw)
    except ValueError as error:
        raise ValueError(f"{name} must be a positive integer") from error
    if value <= 0:
        raise ValueError(f"{name} must be a positive integer")
    return value


def required_environment(name: str) -> str:
    value = os.environ.get(name, "")
    if not value:
        raise ValueError(f"required credential environment variable is empty: {name}")
    return value


def parse_search_result(payload: bytes) -> tuple[int, int, int, int] | None:
    for raw_line in reversed(payload.splitlines()):
        if not raw_line.strip():
            continue
        try:
            record = json.loads(raw_line)
        except json.JSONDecodeError:
            continue
        result = record.get("result")
        if not isinstance(result, dict) or "count" not in result:
            continue
        try:
            count = int(result["count"])
            minimum = int(result.get("min") or 0)
            maximum = int(result.get("max") or 0)
            distinct = int(result.get("distinct") or 0)
        except (TypeError, ValueError):
            return None
        return count, minimum, maximum, distinct
    return None


def submit_event(
    client: PersistentHTTPSClient,
    token: str,
    run_id: str,
    sequence: int,
) -> bool:
    payload = json.dumps(
        {
            "event": {"shc107_run": run_id, "seq": sequence},
            "sourcetype": "_json",
            "index": "main",
        },
        separators=(",", ":"),
    ).encode()
    status, response = client.request(
        "POST",
        "/services/collector/event",
        payload,
        {
            "Authorization": f"Splunk {token}",
            "Connection": "keep-alive",
            "Content-Type": "application/json",
            "Content-Length": str(len(payload)),
        },
    )
    if status != 200:
        return False
    try:
        return json.loads(response).get("code") == 0
    except json.JSONDecodeError:
        return False


def basic_authorization(password: str) -> str:
    encoded = base64.b64encode(f"admin:{password}".encode()).decode()
    return f"Basic {encoded}"


def identify_search_head(
    client: PersistentHTTPSClient,
    password: str,
) -> str | None:
    status, response = client.request(
        "GET",
        "/services/server/info?output_mode=json",
        b"",
        {
            "Authorization": basic_authorization(password),
            "Connection": "keep-alive",
            "Content-Length": "0",
        },
    )
    if status != 200:
        return None
    try:
        entries = json.loads(response).get("entry")
    except json.JSONDecodeError:
        return None
    if not isinstance(entries, list) or not entries:
        return None
    content = entries[0].get("content")
    if not isinstance(content, dict):
        return None
    server_name = content.get("serverName")
    if not isinstance(server_name, str) or not RUN_ID_PATTERN.fullmatch(server_name):
        return None
    return server_name


def search_sequences(
    client: PersistentHTTPSClient,
    password: str,
    run_id: str,
) -> tuple[int, int, int, int] | None:
    form = urllib.parse.urlencode(
        {
            "search": (
                f'search index=main earliest=-24h shc107_run="{run_id}" '
                "| stats count min(seq) as min max(seq) as max "
                "dc(seq) as distinct"
            ),
            "output_mode": "json",
        }
    ).encode()
    status, response = client.request(
        "POST",
        "/services/search/jobs/export",
        form,
        {
            "Authorization": basic_authorization(password),
            "Connection": "keep-alive",
            "Content-Type": "application/x-www-form-urlencoded",
            "Content-Length": str(len(form)),
        },
    )
    if status != 200:
        return None
    return parse_search_result(response)


def stats_text(name: str, stats: ConnectionStats) -> str:
    versions = ",".join(sorted(stats.response_versions)) or "None"
    connection_headers = (
        ",".join(sorted(stats.response_connection_headers)) or "None"
    )
    return (
        f"{name}Connections={stats.opened} "
        f"{name}FirstAttemptFailures={stats.first_attempt_failures} "
        f"{name}RecoveredRequests={stats.recovered_requests} "
        f"{name}ServerCloses={stats.server_closes} "
        f"{name}MaxRequestsPerConnection={stats.max_requests_per_connection} "
        f"{name}ResponseVersions={versions} "
        f"{name}ResponseConnectionHeaders={connection_headers}"
    )


def main() -> int:
    try:
        token = required_environment("HEC_TOKEN")
        password = required_environment("ADMIN_PASSWORD")
        samples = positive_integer("SHC107_SAMPLES", 1800)
        interval = positive_integer("SHC107_INTERVAL_SECONDS", 1)
        settle_attempts = positive_integer("SHC107_SETTLE_ATTEMPTS", 60)
    except ValueError as error:
        print(str(error), file=sys.stderr)
        return 2

    run_id = os.environ.get("SHC107_RUN_ID", os.environ.get("HOSTNAME", "shc107"))
    if not RUN_ID_PATTERN.fullmatch(run_id):
        print("SHC107_RUN_ID contains unsupported characters", file=sys.stderr)
        return 2

    hec_client = PersistentHTTPSClient(
        os.environ.get("SHC107_HEC_SERVICE", "splunk-shcfinal-idxc-indexer-service"),
        8088,
        15,
    )
    search_client = PersistentHTTPSClient(
        os.environ.get("SHC107_SEARCH_SERVICE", "splunk-shcfinal-shc-search-head-service"),
        8089,
        25,
    )

    hec_failures = 0
    search_failures = 0
    identity_failures = 0
    count_regressions = 0
    previous_count = 0
    last_result = (0, 0, 0, 0)
    print(
        f"run={run_id} start={time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())} "
        f"samples={samples} intervalSeconds={interval}",
        flush=True,
    )

    try:
        for sequence in range(1, samples + 1):
            try:
                hec_ok = submit_event(hec_client, token, run_id, sequence)
            except (OSError, http.client.HTTPException):
                hec_ok = False
            if not hec_ok:
                hec_failures += 1

            try:
                search_member = identify_search_head(search_client, password)
            except (OSError, http.client.HTTPException):
                search_member = None
            identity_connection = search_client.stats.opened
            if search_member is None:
                identity_failures += 1
                search_member = "Unavailable"

            try:
                result = search_sequences(search_client, password, run_id)
            except (OSError, http.client.HTTPException):
                result = None
            result_connection = search_client.stats.opened
            if result is None:
                search_failures += 1
                count, minimum, maximum, distinct = last_result
                search_state = "fail"
            else:
                count, minimum, maximum, distinct = result
                last_result = result
                search_state = "ok"
                if count < previous_count:
                    count_regressions += 1
                previous_count = count

            pending = max(0, sequence - count)
            print(
                f"{time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())} "
                f"seq={sequence} hec={'ok' if hec_ok else 'fail'} "
                f"search={search_state} count={count} min={minimum} "
                f"max={maximum} distinct={distinct} pending={pending} "
                f"searchMember={search_member} "
                f"searchMemberConnection={identity_connection} "
                f"searchResultConnection={result_connection} "
                f"{stats_text('hec', hec_client.stats)} "
                f"{stats_text('search', search_client.stats)}",
                flush=True,
            )
            time.sleep(interval)

        complete = False
        for _ in range(settle_attempts):
            try:
                result = search_sequences(search_client, password, run_id)
            except (OSError, http.client.HTTPException):
                result = None
            if result is not None:
                last_result = result
                if result == (samples, 1, samples, samples):
                    complete = True
                    break
            time.sleep(5)
    finally:
        hec_client.close()
        search_client.close()

    count, minimum, maximum, distinct = last_result
    print(
        f"run={run_id} end={time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())} "
        f"submitted={samples} hecFailures={hec_failures} "
        f"searchFailures={search_failures} identityFailures={identity_failures} "
        f"countRegressions={count_regressions} "
        f"finalCount={count} finalMin={minimum} finalMax={maximum} "
        f"finalDistinct={distinct} complete={str(complete).lower()} "
        f"{stats_text('hec', hec_client.stats)} "
        f"{stats_text('search', search_client.stats)}",
        flush=True,
    )
    if hec_failures or search_failures or identity_failures or not complete:
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
