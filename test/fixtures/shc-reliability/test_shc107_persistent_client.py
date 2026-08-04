import http.client
import importlib.util
import pathlib
import sys
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("shc107_persistent_client.py")
SPEC = importlib.util.spec_from_file_location("shc107_persistent_client", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


class FakeSocket:
    pass


class FakeResponse:
    def __init__(
        self,
        status=200,
        payload=b"{}",
        will_close=False,
        version=11,
        connection_header=None,
    ):
        self.status = status
        self.payload = payload
        self.will_close = will_close
        self.version = version
        self.connection_header = connection_header

    def read(self):
        return self.payload

    def getheader(self, name):
        if name.lower() == "connection":
            return self.connection_header
        return None


class FakeConnection:
    def __init__(self, outcomes):
        self.outcomes = outcomes
        self.sock = None
        self.response = None
        self.request_headers = []

    def connect(self):
        self.sock = FakeSocket()

    def request(self, _method, _path, body=None, headers=None):
        del body
        self.request_headers.append(headers)
        outcome = self.outcomes.pop(0)
        if isinstance(outcome, Exception):
            raise outcome
        self.response = outcome

    def getresponse(self):
        return self.response

    def close(self):
        self.sock = None


class FakeFactory:
    def __init__(self, connection_outcomes):
        self.connection_outcomes = list(connection_outcomes)
        self.connections = []

    def __call__(self, _host, _port, timeout, context):
        del timeout, context
        connection = FakeConnection(self.connection_outcomes.pop(0))
        self.connections.append(connection)
        return connection


class PersistentHTTPSClientTest(unittest.TestCase):
    def test_reuses_one_open_connection(self):
        factory = FakeFactory(
            [[FakeResponse(payload=b"one"), FakeResponse(payload=b"two")]]
        )
        client = MODULE.PersistentHTTPSClient(
            "service", 8089, 5, connection_factory=factory
        )

        self.assertEqual((200, b"one"), client.request("POST", "/one", b"", {}))
        self.assertEqual((200, b"two"), client.request("POST", "/two", b"", {}))
        self.assertEqual(1, client.stats.opened)
        self.assertEqual(2, client.stats.max_requests_per_connection)
        self.assertEqual(0, client.stats.first_attempt_failures)
        self.assertEqual({"HTTP/1.1"}, client.stats.response_versions)
        self.assertEqual({"Absent"}, client.stats.response_connection_headers)

    def test_reconnects_and_retries_one_interrupted_request(self):
        factory = FakeFactory(
            [
                [http.client.RemoteDisconnected("closed")],
                [FakeResponse(payload=b"recovered")],
            ]
        )
        client = MODULE.PersistentHTTPSClient(
            "service", 8089, 5, connection_factory=factory
        )

        self.assertEqual(
            (200, b"recovered"),
            client.request("POST", "/search", b"", {}),
        )
        self.assertEqual(2, client.stats.opened)
        self.assertEqual(1, client.stats.first_attempt_failures)
        self.assertEqual(1, client.stats.recovered_requests)

    def test_records_server_close_as_new_connection_boundary(self):
        factory = FakeFactory(
            [
                [
                    FakeResponse(
                        payload=b"one",
                        will_close=True,
                        version=10,
                        connection_header="close",
                    )
                ],
                [FakeResponse(payload=b"two")],
            ]
        )
        client = MODULE.PersistentHTTPSClient(
            "service", 8089, 5, connection_factory=factory
        )

        client.request("POST", "/one", b"", {})
        client.request("POST", "/two", b"", {})
        self.assertEqual(2, client.stats.opened)
        self.assertEqual(1, client.stats.server_closes)
        self.assertEqual({"HTTP/1.0", "HTTP/1.1"}, client.stats.response_versions)
        self.assertEqual(
            {"Absent", "close"},
            client.stats.response_connection_headers,
        )


class SearchResultTest(unittest.TestCase):
    def test_parses_export_result(self):
        payload = (
            b'{"preview":true,"result":{"count":"1"}}\n'
            b'{"preview":false,"result":{"count":"4","min":"1",'
            b'"max":"4","distinct":"4"}}\n'
        )
        self.assertEqual((4, 1, 4, 4), MODULE.parse_search_result(payload))

    def test_rejects_non_numeric_result(self):
        payload = b'{"result":{"count":"not-a-number"}}\n'
        self.assertIsNone(MODULE.parse_search_result(payload))

    def test_records_successful_search_result(self):
        payload = (
            b'{"result":{"count":"4","min":"1",'
            b'"max":"4","distinct":"4"}}\n'
        )
        factory = FakeFactory([[FakeResponse(payload=payload)]])
        client = MODULE.PersistentHTTPSClient(
            "service", 8089, 5, connection_factory=factory
        )

        result = MODULE.search_sequences(client, "password", "run")

        self.assertEqual(MODULE.SearchResult((4, 1, 4, 4), 200, "0"), result)

    def test_reconnects_and_retries_search_detention_405(self):
        payload = (
            b'{"result":{"count":"4","min":"1",'
            b'"max":"4","distinct":"4"}}\n'
        )
        factory = FakeFactory(
            [
                [FakeResponse(status=405, payload=b"detained")],
                [FakeResponse(payload=payload)],
            ]
        )
        client = MODULE.PersistentHTTPSClient(
            "service", 8089, 5, connection_factory=factory
        )

        result = MODULE.search_sequences(client, "password", "run")

        self.assertEqual(MODULE.SearchResult((4, 1, 4, 4), 200, "0"), result)
        self.assertEqual(2, client.stats.opened)
        self.assertEqual(1, client.stats.first_response_failures)
        self.assertEqual(1, client.stats.recovered_requests)
        self.assertEqual(1, client.stats.response_recovered_requests)

    def test_preserves_second_search_detention_405_as_failure(self):
        factory = FakeFactory(
            [
                [FakeResponse(status=405, payload=b"detained")],
                [FakeResponse(status=405, payload=b"detained")],
            ]
        )
        client = MODULE.PersistentHTTPSClient(
            "service", 8089, 5, connection_factory=factory
        )

        result = MODULE.search_sequences(client, "password", "run")

        self.assertEqual(MODULE.SearchResult(None, 405, "HTTPError"), result)
        self.assertEqual(2, client.stats.opened)
        self.assertEqual(1, client.stats.first_response_failures)
        self.assertEqual(0, client.stats.recovered_requests)
        self.assertEqual(0, client.stats.response_recovered_requests)


class HECResultTest(unittest.TestCase):
    def test_records_accepted_hec_response(self):
        factory = FakeFactory([[FakeResponse(payload=b'{"text":"Success","code":0}')]])
        client = MODULE.PersistentHTTPSClient(
            "service", 8088, 5, connection_factory=factory
        )

        result = MODULE.submit_event(client, "token", "run", 1)

        self.assertEqual(MODULE.HECResult(True, 200, "0"), result)

    def test_records_http_rejection_without_response_body(self):
        factory = FakeFactory(
            [
                [FakeResponse(status=503, payload=b"")],
                [FakeResponse(status=503, payload=b"")],
            ]
        )
        client = MODULE.PersistentHTTPSClient(
            "service", 8088, 5, connection_factory=factory
        )

        result = MODULE.submit_event(client, "token", "run", 1)

        self.assertEqual(MODULE.HECResult(False, 503, "HTTPError"), result)

    def test_records_hec_code_rejection(self):
        factory = FakeFactory([[FakeResponse(payload=b'{"text":"Busy","code":9}')]])
        client = MODULE.PersistentHTTPSClient(
            "service", 8088, 5, connection_factory=factory
        )

        result = MODULE.submit_event(client, "token", "run", 1)

        self.assertEqual(MODULE.HECResult(False, 200, "9"), result)

    def test_reconnects_and_retries_explicit_hec_503(self):
        factory = FakeFactory(
            [
                [FakeResponse(status=503, payload=b"")],
                [FakeResponse(payload=b'{"text":"Success","code":0}')],
            ]
        )
        client = MODULE.PersistentHTTPSClient(
            "service", 8088, 5, connection_factory=factory
        )

        result = MODULE.submit_event(client, "token", "run", 1)

        self.assertEqual(MODULE.HECResult(True, 200, "0"), result)
        self.assertEqual(2, client.stats.opened)
        self.assertEqual(1, client.stats.first_response_failures)
        self.assertEqual(1, client.stats.recovered_requests)
        self.assertEqual(1, client.stats.response_recovered_requests)

    def test_preserves_second_hec_503_as_logical_failure(self):
        factory = FakeFactory(
            [
                [FakeResponse(status=503, payload=b"")],
                [FakeResponse(status=503, payload=b"")],
            ]
        )
        client = MODULE.PersistentHTTPSClient(
            "service", 8088, 5, connection_factory=factory
        )

        result = MODULE.submit_event(client, "token", "run", 1)

        self.assertEqual(MODULE.HECResult(False, 503, "HTTPError"), result)
        self.assertEqual(2, client.stats.opened)
        self.assertEqual(1, client.stats.first_response_failures)
        self.assertEqual(0, client.stats.recovered_requests)
        self.assertEqual(0, client.stats.response_recovered_requests)


class SearchHeadIdentityTest(unittest.TestCase):
    def test_reads_server_name_through_persistent_connection(self):
        payload = (
            b'{"entry":[{"content":'
            b'{"serverName":"splunk-stack-search-head-2"}}]}'
        )
        factory = FakeFactory([[FakeResponse(payload=payload)]])
        client = MODULE.PersistentHTTPSClient(
            "service", 8089, 5, connection_factory=factory
        )

        self.assertEqual(
            "splunk-stack-search-head-2",
            MODULE.identify_search_head(client, "password"),
        )
        self.assertEqual(1, client.stats.opened)
        self.assertEqual(
            MODULE.USER_AGENT,
            factory.connections[0].request_headers[0]["User-Agent"],
        )

    def test_rejects_missing_server_name(self):
        factory = FakeFactory([[FakeResponse(payload=b'{"entry":[]}')]])
        client = MODULE.PersistentHTTPSClient(
            "service", 8089, 5, connection_factory=factory
        )

        self.assertIsNone(MODULE.identify_search_head(client, "password"))


if __name__ == "__main__":
    unittest.main()
