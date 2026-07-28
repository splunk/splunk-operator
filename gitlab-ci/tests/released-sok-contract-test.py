#!/usr/bin/env python3
from __future__ import annotations

import importlib.util
import unittest
from pathlib import Path
from unittest import mock


SCRIPT_PATH = Path(__file__).resolve().parents[1] / "released-sok-contract.py"
SPEC = importlib.util.spec_from_file_location("released_sok_contract", SCRIPT_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError(f"Unable to load {SCRIPT_PATH}")
released_sok_contract = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(released_sok_contract)


class ReleasedSokContractTest(unittest.TestCase):
    def test_docker_hub_release_is_verified_then_routed_through_artifactory(self) -> None:
        for registry in released_sok_contract.DOCKER_HUB_REGISTRIES:
            with self.subTest(registry=registry):
                with mock.patch.object(
                    released_sok_contract,
                    "require_image_ref",
                ) as require_image:
                    actual = released_sok_contract.require_released_operator_image(
                        registry,
                        "splunk/splunk-operator",
                        "3.1.0",
                    )

                self.assertEqual(
                    actual,
                    "docker-hub.repo.splunkdev.net/splunk/splunk-operator:3.1.0",
                )
                require_image.assert_called_once_with(actual)

    def test_configured_non_docker_hub_repository_is_preserved(self) -> None:
        actual = released_sok_contract.require_released_operator_image(
            "registry.example.com",
            "team/splunk-operator",
            "3.1.0",
        )

        self.assertEqual(actual, "registry.example.com/team/splunk-operator:3.1.0")

    def test_explicit_artifactory_proxy_repository_is_validated(self) -> None:
        with mock.patch.object(released_sok_contract, "require_image_ref") as require_image:
            actual = released_sok_contract.require_released_operator_image(
                "docker-hub.repo.splunkdev.net",
                "splunk/splunk-operator",
                "3.1.0",
            )

        self.assertEqual(
            actual,
            "docker-hub.repo.splunkdev.net/splunk/splunk-operator:3.1.0",
        )
        require_image.assert_called_once_with(actual)


if __name__ == "__main__":
    unittest.main()
