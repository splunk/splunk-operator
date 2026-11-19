#!/usr/bin/env python3
"""Execute the production Helm hook on a fresh, explicitly selected kind cluster.

Only the readiness resources run: tiny status-controlled CRs replace the operator,
PostgreSQL and KVService workloads. No Splunk images or credentials are needed.
"""
import argparse
import json
import shutil
import subprocess
import tempfile
import time
import uuid
from contextlib import ExitStack
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
RESOURCES = {
    "postgresclusters.platform.splunk.com": ("platform.splunk.com", "v1alpha1", "PostgresCluster", "postgres"),
    "kvservices.enterprise.splunk.com": ("enterprise.splunk.com", "v4", "KVService", "kvservice"),
}


def run(*args, **kwargs):
    return subprocess.run(args, check=True, text=True, capture_output=True, timeout=60, **kwargs).stdout


def eventually(check, description, timeout=90):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if check():
            return
        time.sleep(0.25)
    raise AssertionError(f"Timed out waiting for {description}")


class ReadinessTest:
    release = "readiness"
    hook = "readiness-kvs-ready"

    def __init__(self, context, directory, wait_strategy):
        self.context, self.directory = context, directory
        self.wait_args = [f"--wait={wait_strategy}"] if wait_strategy else []
        self.namespace = f"helm-kvs-{uuid.uuid4().hex[:8]}"
        self.created_crds = []
        self.namespace_created = False
        self.process = None
        self.logs = []

    def kubectl(self, *args, **kwargs):
        return run("kubectl", "--context", self.context, "--request-timeout=15s", *args, **kwargs)

    def get(self, resource, name):
        output = self.kubectl("-n", self.namespace, "get", resource, name, "--ignore-not-found", "-o", "json")
        return json.loads(output) if output.strip() else {}

    def prepare(self):
        print(f"Helm: {run('helm', 'version', '--short').strip()}; wait strategy: {self.wait_args or 'default'}",
              flush=True)
        if not self.context.startswith("kind-") or run("kubectl", "config", "current-context").strip() != self.context:
            raise RuntimeError("Use an explicitly authorized, current kind context with a private KUBECONFIG")
        # Never replace real CRDs, even on an accidentally selected development cluster.
        for resource in RESOURCES:
            if self.kubectl("get", "crd", resource, "--ignore-not-found", "-o", "name").strip():
                raise RuntimeError(f"Fresh cluster required: {resource} already exists")
        for resource, (group, version, kind, _) in RESOURCES.items():
            crd = {
                "apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinition",
                "metadata": {"name": resource},
                "spec": {"group": group, "scope": "Namespaced",
                         "names": {"kind": kind, "plural": resource.split(".")[0]},
                         "versions": [{"name": version, "served": True, "storage": True,
                                       "subresources": {"status": {}},
                                       "schema": {"openAPIV3Schema": {"type": "object", "properties": {
                                           key: {"type": "object", "x-kubernetes-preserve-unknown-fields": True}
                                           for key in ("spec", "status")}}}}]},
            }
            self.kubectl("create", "-f", "-", input=json.dumps(crd))
            self.created_crds.append(resource)
        self.kubectl("wait", "--for=condition=Established", "crd", *self.created_crds, "--timeout=30s")
        self.kubectl("create", "namespace", self.namespace)
        self.namespace_created = True
        chart = self.directory / "chart"
        (chart / "templates").mkdir(parents=True)
        (chart / "Chart.yaml").write_text("apiVersion: v2\nname: splunk-operator\nversion: 0.0.0\n")
        source = ROOT / "helm-chart/splunk-operator"
        # Copy the actual hook and helpers, not a reimplementation or rendered substitute.
        for path in [source / "values.yaml", *source.glob("templates/_*.tpl"),
                     source / "templates/shared_kvservice_readiness.yaml"]:
            shutil.copyfile(path, chart / path.relative_to(source))
        fixtures = []
        for group, version, kind, name in RESOURCES.values():
            fixtures.append(f"apiVersion: {group}/{version}\nkind: {kind}\nmetadata:\n  name: {name}\n"
                            "spec:\n  revision: {{ .Values.fixtureRevision }}\n")
        (chart / "templates/fixtures.yaml").write_text("---\n".join(fixtures))
        self.chart = chart

    def start_helm(self, operation, revision, deadline=120):
        log = self.directory / f"{operation}-{revision}.log"
        self.logs.append(log)
        with log.open("w") as output:
            self.process = subprocess.Popen([
                "helm", operation, self.release, str(self.chart), "--kube-context", self.context,
                "--namespace", self.namespace, "--timeout", "180s",
                "--set", "sharedKVService.enabled=true,sharedKVService.postgres.clusterRef.name=postgres",
                "--set", f"sharedKVService.readiness.timeoutSeconds={deadline},fixtureRevision={revision}",
            ] + self.wait_args, stdout=output, stderr=subprocess.STDOUT, text=True)
        eventually(lambda: bool(self.get("job", self.hook)), "hook Job creation")

    def pending(self, stage):
        # A running wait container proves we are blocked on readiness, not image pulling.
        end = time.monotonic() + 2
        while time.monotonic() < end:
            if self.process.poll() is not None:
                raise AssertionError(f"Helm completed before {stage}: {self.logs[-1].read_text()}")
            time.sleep(0.25)

    def wait_container(self, name):
        def running():
            pods = json.loads(self.kubectl("-n", self.namespace, "get", "pods", "-l",
                                         f"job-name={self.hook}", "-o", "json"))["items"]
            return any(status.get("name") == name and "running" in status.get("state", {})
                       for pod in pods for key in ("initContainerStatuses", "containerStatuses")
                       for status in pod.get("status", {}).get(key, []))
        eventually(running, f"{name} running", timeout=120)

    def ready(self, resource):
        obj = self.get(resource, RESOURCES[resource][3])
        patch = {"status": {"conditions": [{"type": "Ready", "status": "True",
                                            "observedGeneration": obj["metadata"]["generation"]}]}}
        self.kubectl("-n", self.namespace, "patch", resource, obj["metadata"]["name"],
                     "--subresource=status", "--type=merge", "-p", json.dumps(patch))

    def finish(self, success=True):
        result = self.process.wait(timeout=150)
        if (result == 0) != success:
            raise AssertionError(self.logs[-1].read_text())
        status = json.loads(run("helm", "status", self.release, "-n", self.namespace,
                                "--kube-context", self.context, "-o", "json"))["info"]["status"]
        assert status == ("deployed" if success else "failed"), status
        if success:
            eventually(lambda: not self.get("job", self.hook), "hook-succeeded deletion")

    def rbac(self):
        identity = f"system:serviceaccount:{self.namespace}:{self.hook}"
        for resource, (_, _, _, name) in RESOURCES.items():
            for verb in ("get", "list", "watch"):
                assert self.kubectl("auth", "can-i", verb, f"{resource}/{name}", "-n", self.namespace,
                                    "--as", identity).strip() == "yes"
            # Exercise actual API authorization, including name-selector-constrained list.
            self.kubectl("--as", identity, "-n", self.namespace, "get", resource, name)
            self.kubectl("--as", identity, "-n", self.namespace, "get", resource,
                         "--field-selector", f"metadata.name={name}")
            for args in (("get", resource, "unrelated"), ("get", resource),
                         ("patch", resource, name, "--type=merge", "-p", '{"spec":{"revision":99}}')):
                try:
                    self.kubectl("--as", identity, "-n", self.namespace, *args)
                except subprocess.CalledProcessError as error:
                    assert "forbidden" in error.stderr.lower(), error.stderr
                else:
                    raise AssertionError(f"Unexpected readiness ServiceAccount access: {args}")
        try:
            self.kubectl("--as", identity, "-n", self.namespace, "get", "secrets")
        except subprocess.CalledProcessError as error:
            assert "forbidden" in error.stderr.lower(), error.stderr
        else:
            raise AssertionError("Readiness ServiceAccount can read Secrets")

    def execute(self):
        postgres, kvservice = RESOURCES
        self.start_helm("install", 1)
        self.wait_container("wait-postgres")
        self.rbac()
        self.pending("PostgreSQL readiness")
        self.ready(postgres)
        self.wait_container("wait-kvservice")
        self.pending("KVService readiness")
        self.ready(kvservice)
        self.finish()
        print("PASS: install ordering, named-resource RBAC and success cleanup", flush=True)

        self.start_helm("upgrade", 2)
        self.wait_container("wait-postgres")
        for resource, (_, _, _, name) in RESOURCES.items():
            obj = self.get(resource, name)
            assert obj["metadata"]["generation"] == 2
            assert obj["status"]["conditions"][0]["observedGeneration"] == 1
        self.pending("current PostgreSQL generation (old Ready=True remains)")
        self.ready(postgres)
        self.wait_container("wait-kvservice")
        self.pending("current KVService generation (old Ready=True remains)")
        self.ready(kvservice)
        self.finish()
        print("PASS: upgrade rejects stale Ready conditions for both resources", flush=True)

        self.start_helm("upgrade", 3, deadline=30)
        failed_uid = self.get("job", self.hook)["metadata"]["uid"]
        self.finish(success=False)
        assert "DeadlineExceeded" in self.logs[-1].read_text(), self.logs[-1].read_text()
        job = self.get("job", self.hook)
        assert any(c["type"] == "Failed" and c["reason"] == "DeadlineExceeded"
                   for c in job.get("status", {}).get("conditions", [])), job
        print("PASS: Job deadline fails Helm and retains the failed hook", flush=True)

        self.start_helm("upgrade", 4)
        eventually(lambda: self.get("job", self.hook).get("metadata", {}).get("uid") not in (None, failed_uid),
                   "before-hook-creation replacement of failed Job")
        self.ready(postgres)
        self.ready(kvservice)
        self.finish()
        print("PASS: retry replaces failed hook and cleans up after success", flush=True)

    def cleanup(self):
        with ExitStack() as cleanup:
            if self.created_crds:
                cleanup.callback(self.kubectl, "delete", "crd", *self.created_crds,
                                 "--wait=true", "--timeout=30s")
            if self.namespace_created:
                cleanup.callback(self.kubectl, "delete", "namespace", self.namespace,
                                 "--wait=true", "--timeout=30s")
            if self.process and self.process.poll() is None:
                self.process.terminate()
                self.process.wait(timeout=10)
            if self.namespace_created:
                try:
                    run("helm", "status", self.release, "--kube-context", self.context, "-n", self.namespace)
                except subprocess.CalledProcessError as error:
                    if "release: not found" not in error.stderr.lower():
                        raise
                else:
                    run("helm", "uninstall", self.release, "--kube-context", self.context, "-n", self.namespace,
                        "--wait", "--timeout", "30s")
                for resource in ("serviceaccount", "role", "rolebinding"):
                    assert not self.get(resource, self.hook), f"Release-owned {resource} was not removed"


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--context", required=True, help="Authorized fresh kind context (must be current)")
    parser.add_argument("--wait-strategy", choices=("", "legacy", "watcher"), default="",
                        help="Helm 4 wait strategy; omit with Helm 3")
    options = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="helm-kvs-readiness-") as temporary:
        test = ReadinessTest(options.context, Path(temporary), options.wait_strategy)
        try:
            test.prepare()
            test.execute()
        except Exception:
            for log in test.logs:
                print(f"\n{log.name}:\n{log.read_text()}", flush=True)
            if test.namespace_created:
                print(test.kubectl("-n", test.namespace, "get", "jobs,pods", "-o", "yaml"), flush=True)
            raise
        finally:
            test.cleanup()
