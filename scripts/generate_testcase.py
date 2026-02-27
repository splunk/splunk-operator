#!/usr/bin/env python3
import argparse
import json
import os
import re
import shutil
import sys
from datetime import datetime
from pathlib import Path


def load_spec(path: Path):
    suffix = path.suffix.lower()
    if suffix == ".json":
        with path.open("r", encoding="utf-8") as f:
            return json.load(f)
    if suffix in (".yaml", ".yml"):
        try:
            import yaml  # type: ignore
        except Exception:
            print("[ERROR] PyYAML is required to read YAML specs.")
            print("Install with: python3 -m pip install pyyaml")
            sys.exit(2)
        with path.open("r", encoding="utf-8") as f:
            return yaml.safe_load(f)
    print(f"[ERROR] Unsupported spec extension: {suffix}")
    print("Use .yaml, .yml, or .json")
    sys.exit(2)


def slugify(value: str) -> str:
    value = value.strip().lower()
    value = re.sub(r"[^a-z0-9]+", "-", value)
    value = re.sub(r"-+", "-", value).strip("-")
    return value or "test"


def ensure_dir(path: Path):
    path.mkdir(parents=True, exist_ok=True)


def read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def write_text(path: Path, content: str, force: bool):
    if path.exists() and not force:
        print(f"[ERROR] Refusing to overwrite existing file: {path}")
        print("Use --force to overwrite")
        sys.exit(1)
    path.write_text(content, encoding="utf-8")


def indent_block(text: str, spaces: int) -> str:
    prefix = " " * spaces
    return "\n".join(prefix + line if line.strip() else "" for line in text.splitlines())


def go_bool(value: bool) -> str:
    return "true" if value else "false"


def kuttl_assert_for_resource(res: dict) -> str:
    api_version = res.get("apiVersion", "")
    kind = res.get("kind", "")
    name = res.get("name", "")
    status = res.get("status")
    lines = ["---", f"apiVersion: {api_version}", f"kind: {kind}", "metadata:", f"  name: {name}"]
    if isinstance(status, dict) and status:
        lines.append("status:")
        for key, value in status.items():
            if isinstance(value, str):
                lines.append(f"  {key}: {value}")
            else:
                lines.append(f"  {key}: {value}")
    return "\n".join(lines) + "\n"


def generate_kuttl(spec: dict, repo_root: Path, force: bool, dry_run: bool):
    suite = spec["suite"]
    name = spec["name"]
    crs = spec.get("crs")
    if crs is None:
        cr = spec.get("cr")
        if cr is None:
            crs = []
        else:
            crs = [cr]
    if not isinstance(crs, list):
        print("[ERROR] crs must be a list")
        sys.exit(1)
    upgrade = spec.get("upgrade", {}) if isinstance(spec.get("upgrade", {}), dict) else {}
    upgrade_enabled = bool(upgrade.get("enabled", False))
    if not crs and not upgrade_enabled:
        print("[ERROR] kuttl spec requires cr or crs unless upgrade.enabled=true")
        sys.exit(1)
    expected = spec.get("expected", {})
    resources = spec.get("resources", [])
    phase = expected.get("phase", "Ready")
    phases = expected.get("phases", {})
    assert_path = expected.get("assert_path", "")

    test_dir = repo_root / "kuttl" / "tests" / suite / name
    assert_name = "00-assert.yaml" if not upgrade_enabled else "04-assert.yaml"
    assert_target = test_dir / assert_name

    if dry_run:
        print(f"[DRY-RUN] Create {test_dir}")
        if upgrade_enabled:
            print(f"[DRY-RUN] Write {test_dir / '00-install.yaml'}")
            print(f"[DRY-RUN] Write {test_dir / '01-assert-operator-ready.yaml'}")
            print(f"[DRY-RUN] Write {test_dir / '02-upgrade.yaml'}")
            print(f"[DRY-RUN] Write {test_dir / '03-assert-operator-image.yaml'}")
        for index, cr in enumerate(crs):
            kind = cr.get("kind", "")
            deploy_index = index if not upgrade_enabled else index + 4
            deploy_name = f"{deploy_index:02d}-deploy-{slugify(kind)}.yaml"
            cr_path = Path(cr.get("path", "")).expanduser()
            if not cr_path.is_absolute():
                cr_path = (repo_root / cr_path).resolve()
            print(f"[DRY-RUN] Copy {cr_path} -> {test_dir / deploy_name}")
        print(f"[DRY-RUN] Write {assert_target}")
        return

    ensure_dir(test_dir)

    if upgrade_enabled:
        method = str(upgrade.get("method", "helm")).lower()
        if method != "helm":
            print("[ERROR] upgrade.method only supports 'helm' for now")
            sys.exit(1)

        helm_release = str(upgrade.get("helmRelease", "splunk-test"))
        helm_repo_env = str(upgrade.get("helmChartPathEnv", "HELM_REPO_PATH"))
        namespace_env = str(upgrade.get("namespaceEnv", "NAMESPACE"))
        values_file = str(upgrade.get("valuesFile", "")).strip()
        operator_image_env = str(upgrade.get("operatorImageEnv", "KUTTL_SPLUNK_OPERATOR_IMAGE"))
        operator_image_new_env = str(upgrade.get("operatorImageNewEnv", "KUTTL_SPLUNK_OPERATOR_NEW_IMAGE"))
        enterprise_image_env = str(upgrade.get("enterpriseImageEnv", "KUTTL_SPLUNK_ENTERPRISE_IMAGE"))
        enterprise_image_new_env = str(upgrade.get("enterpriseImageNewEnv", "KUTTL_SPLUNK_ENTERPRISE_NEW_IMAGE"))
        extra_args = upgrade.get("extraHelmArgs", [])
        if not isinstance(extra_args, list):
            extra_args = []

        values_arg = ""
        if values_file:
            values_path = Path(values_file).expanduser()
            if not values_path.is_absolute():
                values_path = (repo_root / values_path).resolve()
            if not values_path.exists():
                print(f"[ERROR] valuesFile not found: {values_path}")
                sys.exit(1)
            values_target = test_dir / values_path.name
            if not values_target.exists() or force:
                shutil.copyfile(values_path, values_target)
            values_arg = f"-f {values_target.name}"

        extra = " ".join(extra_args)
        install_cmd = (
            f"helm install {helm_release} "
            f"${{{helm_repo_env}}}/splunk-enterprise {values_arg} "
            f"--set splunk-operator.splunkOperator.image.repository=${{{operator_image_env}}} "
            f"--set splunk-operator.image.repository=${{{enterprise_image_env}}} "
            f"--namespace ${{{namespace_env}}} "
            f"--set splunk-operator.splunkOperator.splunkGeneralTerms=\\\"--accept-sgt-current-at-splunk-com\\\" "
            f"{extra}"
        ).strip()

        upgrade_cmd = (
            f"helm upgrade {helm_release} "
            f"${{{helm_repo_env}}}/splunk-enterprise --reuse-values {values_arg} "
            f"--set splunk-operator.splunkOperator.image.repository=${{{operator_image_new_env}}} "
            f"--set splunk-operator.image.repository=${{{enterprise_image_new_env}}} "
            f"--namespace ${{{namespace_env}}} "
            f"{extra}"
        ).strip()

        install_step = f\"\"\"---\napiVersion: kuttl.dev/v1beta1\nkind: TestStep\ncommands:\n  - command: {install_cmd}\n    namespaced: true\n\"\"\"\n+        ready_assert = \"\"\"---\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: splunk-operator-controller-manager\nstatus:\n  readyReplicas: 1\n  availableReplicas: 1\n\"\"\"\n+        upgrade_step = f\"\"\"---\napiVersion: kuttl.dev/v1beta1\nkind: TestStep\ncommands:\n  - command: {upgrade_cmd}\n    namespaced: true\n\"\"\"\n+        image_check_cmd = (\n+            f\"kubectl -n ${{{namespace_env}}} get deploy splunk-operator-controller-manager \"\n+            f\"-o jsonpath='{{{{.spec.template.spec.containers[?(@.name==\\\\\\\"manager\\\\\\\")].image}}}}' \"\n+            f\"| grep -q \\\"${{{operator_image_new_env}}}\\\"\"\n+        )\n+        image_assert_step = f\"\"\"---\napiVersion: kuttl.dev/v1beta1\nkind: TestStep\ncommands:\n  - command: {image_check_cmd}\n    namespaced: true\n\"\"\"\n+\n+        write_text(test_dir / \"00-install.yaml\", install_step, force)\n+        write_text(test_dir / \"01-assert-operator-ready.yaml\", ready_assert, force)\n+        write_text(test_dir / \"02-upgrade.yaml\", upgrade_step, force)\n+        write_text(test_dir / \"03-assert-operator-image.yaml\", image_assert_step, force)\n+
    for index, cr in enumerate(crs):
        api_version = cr.get("apiVersion", "")
        kind = cr.get("kind", "")
        cr_name = cr.get("name", "")
        cr_path = Path(cr.get("path", "")).expanduser()
        if not api_version or not kind or not cr_name:
            print("[ERROR] crs entries must include apiVersion, kind, and name")
            sys.exit(1)
        if not cr_path.is_absolute():
            cr_path = (repo_root / cr_path).resolve()
        if not cr_path.exists():
            print(f"[ERROR] CR manifest not found: {cr_path}")
            sys.exit(1)
        deploy_index = index if not upgrade_enabled else index + 4
        deploy_name = f"{deploy_index:02d}-deploy-{slugify(kind)}.yaml"
        deploy_target = test_dir / deploy_name
        if deploy_target.exists() and not force:
            print(f"[ERROR] Deploy file exists: {deploy_target}")
            sys.exit(1)
        shutil.copyfile(cr_path, deploy_target)

    # Build assert content
    content = []
    for cr in crs:
        api_version = cr.get("apiVersion", "")
        kind = cr.get("kind", "")
        cr_name = cr.get("name", "")
        if not api_version or not kind or not cr_name:
            print("[ERROR] crs entries must include apiVersion, kind, and name")
            sys.exit(1)
        phase_for_cr = phases.get(cr_name, phase) if isinstance(phases, dict) else phase
        content.append("---")
        content.append(f"apiVersion: {api_version}")
        content.append(f"kind: {kind}")
        content.append("metadata:")
        content.append(f"  name: {cr_name}")
        content.append("status:")
        content.append(f"  phase: {phase_for_cr}")
        content.append("")

    if assert_path:
        assert_file = Path(assert_path).expanduser()
        if not assert_file.is_absolute():
            assert_file = (repo_root / assert_file).resolve()
        if not assert_file.exists():
            print(f"[ERROR] assert_path not found: {assert_file}")
            sys.exit(1)
        content.append(read_text(assert_file).rstrip())
        content.append("")
    elif resources:
        for res in resources:
            content.append(kuttl_assert_for_resource(res).rstrip())
    elif crs:
        content.append("# TODO: add resource assertions (StatefulSet, Service, Secret, etc.)")

    if content:
        write_text(assert_target, "\n".join(content).rstrip() + "\n", force)

    readme_path = test_dir / "readme.txt"
    if not readme_path.exists():
        readme_path.write_text(f"KUTTL test: {name}\n", encoding="utf-8")

    print(f"[OK] Created KUTTL test: {test_dir}")


def suite_template(suite: str) -> str:
    suite_name = slugify(suite)
    return f"""// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the \"License\");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//\thttp://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an \"AS IS\" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package {suite_name}

import (
    "testing"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    "github.com/splunk/splunk-operator/test/testenv"
)

const (
    PollInterval = 5 * time.Second
    ConsistentPollInterval = 200 * time.Millisecond
    ConsistentDuration = 2000 * time.Millisecond
)

var (
    testenvInstance *testenv.TestEnv
    testSuiteName   = "{suite_name}-" + testenv.RandomDNSName(3)
)

func TestBasic(t *testing.T) {{
    RegisterFailHandler(Fail)
    RunSpecs(t, "Running "+testSuiteName)
}}

var _ = BeforeSuite(func() {{
    var err error
    testenvInstance, err = testenv.NewDefaultTestEnv(testSuiteName)
    Expect(err).ToNot(HaveOccurred())
}})

var _ = AfterSuite(func() {{
    if testenvInstance != nil {{
        Expect(testenvInstance.Teardown()).ToNot(HaveOccurred())
    }}
}})
"""


def integration_flow(spec: dict):
    arch = spec.get("architecture", {}) if isinstance(spec.get("architecture", {}), dict) else {}
    arch_name = str(arch.get("name", "")).upper()
    indexer_replicas = int(arch.get("indexerReplicas", 3) or 3)
    site_count = int(arch.get("siteCount", 3) or 3)
    shc = bool(arch.get("shc", True))
    use_cluster_master = bool(arch.get("useClusterMaster", False))
    features = spec.get("features", {}) if isinstance(spec.get("features", {}), dict) else {}
    use_smartstore = bool(features.get("smartstore", False))
    use_appframework = bool(features.get("appframework", False))

    deploy_lines = []
    ready_lines = []
    notes = []

    upgrade = spec.get("upgrade", {}) if isinstance(spec.get("upgrade", {}), dict) else {}
    upgrade_enabled = bool(upgrade.get("enabled", False))
    operator_image_env = str(upgrade.get("operatorImageNewEnv", "UPGRADE_OPERATOR_IMAGE"))
    enterprise_image_env = str(upgrade.get("enterpriseImageNewEnv", "UPGRADE_SPLUNK_IMAGE"))
    upgrade_lines = []

    validations = spec.get("validations", {}) if isinstance(spec.get("validations", {}), dict) else {}
    sva = str(validations.get("sva", "")).strip().upper()
    has_mc_flag = isinstance(validations, dict) and "monitoringConsole" in validations
    has_lm_flag = isinstance(validations, dict) and "licenseManager" in validations
    mc_enabled = bool(validations.get("monitoringConsole", False))
    lm_enabled = bool(validations.get("licenseManager", False))
    mc_name_override = str(validations.get("monitoringConsoleName", "")).strip()
    if sva in ("C3", "SINGLE-SITE", "SINGLE_SITE", "SVA") and arch_name in ("C3", "SINGLE-SITE", "SINGLE_SITE"):
        if not has_mc_flag:
            mc_enabled = True
        if not has_lm_flag:
            lm_enabled = True

    go_shc = go_bool(shc)

    if arch_name in ("", "S1", "STANDALONE"):
        if use_smartstore:
            deploy_lines.append('Skip("TODO: implement smartstore standalone using DeployStandaloneWithGivenSmartStoreSpec")')
        elif use_appframework:
            deploy_lines.append('Skip("TODO: implement app framework standalone (no helper) using custom spec")')
        else:
            deploy_lines.append('instance, err := deployment.DeployStandalone(ctx, deployment.GetName(), "", "")')
            deploy_lines.append('Expect(err).To(Succeed(), "Unable to deploy standalone instance")')
            ready_lines.append("testenv.StandaloneReady(ctx, deployment, deployment.GetName(), instance, testcaseEnvInst)")
    elif arch_name in ("C3", "SINGLE-SITE", "SINGLE_SITE"):
        if use_appframework:
            deploy_lines.append('Skip("TODO: implement app framework using DeploySingleSiteClusterWithGivenAppFrameworkSpec")')
        elif use_smartstore:
            deploy_lines.append('Skip("TODO: implement smartstore using DeployClusterManagerWithSmartStoreIndexes + DeployIndexerCluster")')
        else:
            if mc_enabled:
                mc_ref_expr = "deployment.GetName()"
                if mc_name_override:
                    mc_ref_expr = json.dumps(mc_name_override)
                deploy_lines.append(f"mcRef := {mc_ref_expr}")
                deploy_lines.append('lmRef := ""')
                if lm_enabled:
                    deploy_lines.append("lmRef = deployment.GetName()")
            deploy_lines.append(
                f'err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), {indexer_replicas}, {go_shc}, {"mcRef" if mc_enabled else "\"\""})'
            )
            deploy_lines.append('Expect(err).To(Succeed(), "Unable to deploy single-site cluster")')
        ready_lines.append(
            "testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)" if use_cluster_master else "testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)"
        )
        if shc:
            ready_lines.append("testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)")
        ready_lines.append("testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)")
        ready_lines.append("testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)")
        if lm_enabled:
            ready_lines.append(
                "testenv.LicenseMasterReady(ctx, deployment, testcaseEnvInst)" if use_cluster_master else "testenv.LicenseManagerReady(ctx, deployment, testcaseEnvInst)"
            )
            notes.append("License Manager readiness requires a license file/configmap to be configured for the test env.")
        if mc_enabled:
            ready_lines.append('mc, err := deployment.DeployMonitoringConsole(ctx, mcRef, lmRef)')
            ready_lines.append('Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")')
            ready_lines.append("testenv.VerifyMonitoringConsoleReady(ctx, deployment, mcRef, mc, testcaseEnvInst)")
    elif arch_name in ("M4", "MULTISITE", "MULTI-SITE", "M4-SHC", "MULTISITE-SHC"):
        if use_appframework:
            deploy_lines.append('Skip("TODO: implement app framework using DeployMultisiteClusterWithSearchHeadAndAppFramework")')
        elif use_smartstore:
            deploy_lines.append('Skip("TODO: implement smartstore using DeployMultisiteClusterWithSearchHeadAndIndexes")')
        else:
            deploy_lines.append(
                f'err := deployment.DeployMultisiteClusterWithSearchHead(ctx, deployment.GetName(), {indexer_replicas}, {site_count}, "")'
            )
            deploy_lines.append('Expect(err).To(Succeed(), "Unable to deploy multisite cluster with SHC")')
        ready_lines.append(
            "testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)" if use_cluster_master else "testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)"
        )
        ready_lines.append(f"testenv.IndexersReady(ctx, deployment, testcaseEnvInst, {site_count})")
        ready_lines.append(f"testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, {site_count})")
        ready_lines.append("testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)")
        ready_lines.append("testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)")
    elif arch_name in ("M1", "MULTISITE-NOSHC", "MULTISITE_NO_SHC", "MULTISITE-NO-SHC"):
        if use_appframework:
            deploy_lines.append('Skip("TODO: implement app framework using DeployMultisiteClusterWithSearchHeadAndAppFramework (shc=false)")')
        elif use_smartstore:
            deploy_lines.append('Skip("TODO: implement smartstore multisite without SHC (no helper) - use custom flow")')
        else:
            deploy_lines.append(
                f'err := deployment.DeployMultisiteCluster(ctx, deployment.GetName(), {indexer_replicas}, {site_count}, "")'
            )
            deploy_lines.append('Expect(err).To(Succeed(), "Unable to deploy multisite cluster")')
        ready_lines.append(
            "testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)" if use_cluster_master else "testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)"
        )
        ready_lines.append(f"testenv.IndexersReady(ctx, deployment, testcaseEnvInst, {site_count})")
        ready_lines.append(f"testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, {site_count})")
        ready_lines.append("testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)")
    else:
        deploy_lines.append('Skip("TODO: unsupported architecture in generator. Update spec or add helper mapping.")')
        notes.append("Use docs/agent/TESTCASE_PATTERNS.md to pick the right helper.")

    if deploy_lines and deploy_lines[0].startswith("Skip("):
        ready_lines = ["// TODO: add readiness checks once deployment is implemented"]

    if upgrade_enabled:
        upgrade_lines = [
            f'operatorImage := os.Getenv("{operator_image_env}")',
            'Expect(operatorImage).ToNot(BeEmpty())',
            'err = testcaseEnvInst.UpdateOperatorImage(operatorImage)',
            'Expect(err).To(Succeed(), "Unable to update operator image")',
            'testenv.VerifyOperatorImage(ctx, testcaseEnvInst, operatorImage)',
            '',
            f'splunkImage := os.Getenv("{enterprise_image_env}")',
            'Expect(splunkImage).ToNot(BeEmpty())',
            '// TODO: update CR spec image to splunkImage and wait for reconciliation',
            '// TODO: verify splunk pod images updated',
            'testenv.VerifySplunkPodImagesContain(testcaseEnvInst.GetName(), splunkImage)',
        ]

    return deploy_lines, ready_lines, upgrade_lines, notes, upgrade_enabled


def integration_template(spec: dict) -> str:
    suite = slugify(spec["suite"])
    name = spec["name"]
    arch = spec.get("architecture", {}) if isinstance(spec.get("architecture", {}), dict) else {}
    arch_name = str(arch.get("name", "")).upper() or "Custom"
    cr = spec.get("cr", {}) if isinstance(spec.get("cr", {}), dict) else {}
    kind = cr.get("kind", "") or arch_name
    cr_path = cr.get("path", "")

    deploy_lines, ready_lines, upgrade_lines, notes, upgrade_enabled = integration_flow(spec)
    deploy_snippet = indent_block("\n".join(deploy_lines), 12)
    ready_snippet = indent_block("\n".join(ready_lines), 12)
    upgrade_snippet = ""
    post_upgrade_ready = ""
    if upgrade_lines:
        upgrade_snippet = indent_block("\n".join(upgrade_lines), 12)
        post_upgrade_ready = ready_snippet
    notes_snippet = ""
    if notes:
        notes_snippet = indent_block("\n".join([f\"// NOTE: {n}\" for n in notes]), 12)
    extra_imports = ""
    if upgrade_enabled:
        extra_imports = "    \"os\"\\n"

    return f"""// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the \"License\");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//\thttp://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an \"AS IS\" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package {suite}

import (
    "context"
    "fmt"
{extra_imports}

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "github.com/onsi/ginkgo/v2/types"

    "github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("{kind} integration test", func() {{
    var testcaseEnvInst *testenv.TestCaseEnv
    var deployment *testenv.Deployment
    ctx := context.TODO()

    BeforeEach(func() {{
        var err error
        name := fmt.Sprintf("%s-%s", testenvInstance.GetName(), testenv.RandomDNSName(3))
        testcaseEnvInst, err = testenv.NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
        Expect(err).To(Succeed(), "Unable to create testcaseenv")
        deployment, err = testcaseEnvInst.NewDeployment(testenv.RandomDNSName(3))
        Expect(err).To(Succeed(), "Unable to create deployment")
    }})

    AfterEach(func() {{
        if types.SpecState(CurrentSpecReport().State) == types.SpecStateFailed {{
            testcaseEnvInst.SkipTeardown = true
        }}
        if deployment != nil {{
            deployment.Teardown()
        }}
        if testcaseEnvInst != nil {{
            Expect(testcaseEnvInst.Teardown()).ToNot(HaveOccurred())
        }}
    }})

    Context("{name}", func() {{
        It("integration, {kind}: {name}", func() {{
            // Architecture: {arch_name}
            // Spec path: {cr_path}
{notes_snippet}
{deploy_snippet}

            {ready_snippet}

{upgrade_snippet}

{post_upgrade_ready}

            // TODO: verify resources (pods, services, statefulsets, secrets)
        }})
    }})
}})
"""


def generate_integration(spec: dict, repo_root: Path, force: bool, dry_run: bool):
    suite = slugify(spec["suite"])
    name = slugify(spec["name"]).replace("-", "_")

    suite_dir = repo_root / "test" / suite
    test_file = suite_dir / f"{name}_test.go"
    suite_file = suite_dir / f"{suite}_suite_test.go"

    if dry_run:
        print(f"[DRY-RUN] Create {suite_dir}")
        print(f"[DRY-RUN] Write {test_file}")
        if not suite_file.exists():
            print(f"[DRY-RUN] Write {suite_file}")
        return

    ensure_dir(suite_dir)

    if not suite_file.exists():
        write_text(suite_file, suite_template(suite), force=False)
        print(f"[OK] Created suite file: {suite_file}")

    write_text(test_file, integration_template(spec), force)
    print(f"[OK] Created integration test: {test_file}")


def main():
    parser = argparse.ArgumentParser(description="Generate KUTTL or integration test scaffolds from a spec file.")
    parser.add_argument("--spec", required=True, help="Path to testcase spec (.yaml/.yml/.json)")
    parser.add_argument("--force", action="store_true", help="Overwrite existing files")
    parser.add_argument("--dry-run", action="store_true", help="Print actions without writing files")
    args = parser.parse_args()

    spec_path = Path(args.spec).expanduser().resolve()
    if not spec_path.exists():
        print(f"[ERROR] Spec file not found: {spec_path}")
        sys.exit(1)

    spec = load_spec(spec_path)
    if not isinstance(spec, dict):
        print("[ERROR] Spec must be a dictionary")
        sys.exit(1)

    required = ["type", "suite", "name"]
    for key in required:
        if key not in spec:
            print(f"[ERROR] Missing required field: {key}")
            sys.exit(1)

    cr = spec.get("cr")
    crs = spec.get("crs")
    if cr is None and crs is None:
        print("[ERROR] spec must include cr or crs")
        sys.exit(1)
    if cr is not None:
        if not isinstance(cr, dict):
            print("[ERROR] cr must be an object")
            sys.exit(1)
        if "path" not in cr:
            print("[ERROR] cr.path is required")
            sys.exit(1)
    if crs is not None and not isinstance(crs, list):
        print("[ERROR] crs must be a list")
        sys.exit(1)

    repo_root = Path(__file__).resolve().parent.parent
    test_type = str(spec["type"]).lower()

    if test_type == "kuttl":
        generate_kuttl(spec, repo_root, args.force, args.dry_run)
    elif test_type in ("integration", "ginkgo"):
        generate_integration(spec, repo_root, args.force, args.dry_run)
    else:
        print(f"[ERROR] Unknown test type: {test_type}")
        print("Use 'kuttl' or 'integration'")
        sys.exit(1)


if __name__ == "__main__":
    main()
