38fc4fab Merge pull request #1736 from splunk/CSPL-4575-fix-gh-workflow
b9ef976f CSPL-4575 Fix broken gh workflow
7fa86989 Fix app_tel_for_sok app.conf install stanza header (#1724)
76f6658e Merge pull request #1722 from splunk/fix/remove-stale-crd-copy-step
2e65b5db adding parallel work for formatting and unit test
d9eaecbb ci: make Coveralls upload non-blocking during outages
e4cd5b77 Fix bundle target after Helm CRD removal
528ac316 Add sqs_smartubus_cp to providers (#1716)
e3f19b9d Merge pull request #1709 from splunk/feature/updatetimer
8f41f636 CSPL-3549 Splunk Operator Enhancement – Ingestion and Indexing Separation (#1550)
9dc8b62a update comment
c2a877df Update requeue timer from 1 day to 6 hour
d413ee89 Merge pull request #1677 from splunk/feature/telemetry1
02eb7aa4 fix
c059cf32 upgrade golang version to 1.25.7 (#1705)
d7d9a71e Resolve merge conflict
24a92caf Merge pull request #1697 from splunk/CSPL-4372-add-contribution-testing-process
3f703217 Merge pull request #1702 from splunk/CSPL-4375-exclude-tests-when-not-needed
2e9abb99 Adjust paths for ignored files in pull requests and pushes for clarity.
ce572082 CSPL-4375 don't execute tests on non-relevant changes
5306a09a Set test to false before release
cef4f680 CSPL-4372: docs: add maintainer workflow for external contributions in CONTRIBUTING.md
e47ab3fa Merge pull request #1694 from splunk/fix-unit-tests
c731e5e4 refactor: rename function for clarity in secrets.go
77df63f9 Validation webhook implementation (#1682)
c977bdcb more tests
77df860f fix
06ca9525 resolve conflicts
9e6903b0 Add more tests
b879d5f5 CSPL-4324 Emitting events for passwords, secrets, app fw, CM, scaling and smart store (#1689)
4011c5b4 CSPL-4344: Add License Expired Event (#1679)
23ac8e85 fix unit test
bb988ff7 Increase test coverage
76a01101 Address comments
4a7305c7 fix: fix long execution time unit tests
a350d593 Add agents.md file (#1675)
7e2f0d00 Merge pull request #1692 from splunk/vuln-61143-openetelemetry-go
dd931e42 Merge branch 'develop' into vuln-61143-openetelemetry-go
533a09cc CSPL-4510: ubi8, golang version upgrades (#1690)
7cec815f vuln-61143 upgrade opentelemtry-go from 1.33 to 1.40
04d7ad13 Merge pull request #1673 from splunk/make-cla-check-clearer
9c8dcbd7 Set version in make
0009f507 fix int test
381df2c9 fix int test
dc08f44a Address comment for renaming sok app and fix
82480ef7 fix
2dc1851f Fix unit test
d0e0f5e0 Update deployment telemetry
ce04e3b1 Update CLA check workflow
ddd31867 fix
bcf54341 Address some comments
f7c5c881 Set value for test and sokVersion
8829919d Merge pull request #1642 from splunk/CSPL-3964_Upgrade-operator-sdk
7092e25c cleanup
a01170da fix
694e7666 Pass test mode as false in testing
930784eb fix test
4db7a4a1 CSPL-3964: removing reference to branch for GH int-test-workflow
656737bf Add more unit tests
075c6d68 CSPL-4337: Migrate k8s events to recorder (#1653)
edf26189 Initial commit
6d82816c Merge pull request #1668 from splunk/feature/ISSUE-1661-missing-inputs-helm
463cd880 Merge pull request #1637 from ductrung-nguyen/fix/code-format
e5d9ec81 CSPL-3964: adding back some webhook-related changes that were missed in prior commit
19b677ec ISSUE-1661 Address comments
620a05f5 CSPL-3964: adding golangci linter to project, temporarily with a ton of exclusion rules, but the goal is to remove these as we work through them, as well as adding TLS support for metrics and webhooks (even if this won't yet be used)
21a4f7dd support AWS S3 region parsing for 4-part regions (#1602)
d22dabed ISSUE-1661 Add deployerNodeAffinity and deployerResourceSpec to SHC helm chart
f8995fad Add a check if contributor has signed Splunk Contributor License Agreement (#1640)
5fcc6858 CSPL-3964: Rebasing off of latest develop with golang upgrade changes as well as removing some uneccessary env vars added to the workflows.
efb89780 CSPL-3964: Upgrading operator sdk.
b5b35a9f Merge pull request #1625 from splunk/CSPL-3918_UpgradeGolangVersion
3042251b CSPL-3918: Upgrading minor from 1.25.4 to 1.25.5, as this is the most recent stable release.
927ac52b CSPL-3918: Fixing issues with smoke tests.
88d34ed6 CSPL-3918: Updating golang version in Dockerfile.
ffc5bc93 CSPL-3928: Fixing issue with incompatible CONTROLLER_TOOLS_VERSION.
a2076f80 CSPL-3918: Upgrading to golang version 1.25 vs 1.24.
6cd2e79b CSPL-3918: Adding go.mod to .biased_lang_exclude.
6dc6a549 CSPL-3918: Upgrading GoLang version due to security vulnerability.
6814c012 Merge pull request #1630 from splunk/CSPL-4201-use-OIDC-in-github-pipelines
6b371c46 add link to required value for SGT, fix Jekyll hyperlinks (#1635)
f92da7b9 style: fix code format with gofmt
93c4e921 update CONTRIBUTING.md file (#1632)
85bb5936 Fix misspelled timeout minutes, remove trailing whhitespaces
a6db6084 CSPL-4239: Stabilize MC Error State (#1631)
3314a75b Merge pull request #1629 from splunk/CSPL-4281-add-error-handling-for-cm-multisite-check
bf1129a5 Remove CSPL-4201-pipeline-tests-base branch from multiple GitHub Actions [added for debug]
7249a86e Fix: pipelines not passing on not-supported Ubuntu version.
d472b2bf Update skipLogOutput setting in S3 bucket creation scripts for helm tests
a84be419 Update GitHub Actions workflows to use environment variables for S3 credentials
594ee3cf CSPL-4201 job timeouts - reduce them on step level and adjust it to observed values
836c0309 CSPL-4201 Update GitHub Actions workflow to add id-token permission
d0037278 CSPL-4201 Enhance GitHub Actions workflows with role duration and timeout settings
480eba4c Update GitHub Actions workflows to include CSPL-4201-pipeline-tests-base branch
81a5a893 CSPL-4201 Update GitHub Actions workflows to use OIDC tokens instead of static credentials
b5d8704a Merge pull request #1627 from splunk/CSPL-4281-fix-stale-peer-cm-config
159f6fd3 Merge pull request #1628 from splunk/CSPL-4282-fix-pipeline-misconfiguration
de6b4ea1 Fix: missing VerifyCMisMultisiteCall to GetCMMultisiteEnvVarsCall rename
a4f702ef CAPL-4281 Add fallback error handling for cluster manager multisite check
f39eefbb Add EKS Test Cluster Name Action and update workflows
b8487605 CSPL-4282 Update Makefile for Jekyll documentation preview and enhance test script output verbosity
7845d874 SCPL-4282 Fix: pipelines race condition and pipelines misconfiguration
f3da6168 CSPL-4281 Fix: MC crash loop during scale-down - update peer config immediately
64ce364e Merge pull request #1626 from splunk/fix_helm_m4
a00be5df remove branch from workflow
328d3ac6 CSPL-4125: Promote multi-platform images in release (#1617)
658a520a run helm test in workflow on branch push
827901d8 Fix m4 helm tests by updating the zones for new subnets
44f74f60 fix isAppAlreadyInstalled error handling (#1608)
d7abbf5c CSPL-4207: Move test yamls into fixtures (#1607)
5353d6b9 CSPL-4238: Upgrade golang.org/x/crypto from 0.39.0 to 0.43.0 (#1619)
fd79d9de Merge pull request #1615 from splunk/CSPL-4237-improve-github-page
9a23c8f6 CSPL-4237 Update .gitignore
9092b4b5 CSPL-4237 Add local documentation preview command to Makefile and update Examples.md with kubectl command for decoding secrets.
100afc48 CSPL-4237 Remove GitHub Actions workflow for documentation preview
8bd8fa53 CSPL-4237 Fix pipeline for preview
8275e78d CSPL-4237 Enable GitHub documentation previews
b3112506 Revert "CSPL-4237 Improve documentation pages"
6256f99b CSPL-4237 Improve documentation pages
0cf7195b CSPL-4237 Improve documentation pages
72dc0e62 CSPL-4208: Fix aks integration test setup (#1609)
9c9902b3 CSPL-4132: Update ubi-minimal image (#1605)
ce192e1c CSPL-4123: Update install and upgrade examples  (#1601)
06230b35 Merge pull request #1599 from splunk/CSPL-4113-k8s-update
8453acb6 CSPL-4113 Remove branch from tests
cc31a210 tests
bcd53138 CSPL-4113 k8s upgrade
a02abb9a CSPL-4026: Update app framework documentation to deploy apps with multiple scopes (#1598)
b9523c8d CSPL-4024: Enchance IDXC upgrade error message.  (#1594)
856ee289 Properly process peers for MC(#1597)
f1f470fa CSPL-4007: Add sample CRD yaml files in docs (#1587)
4d99fa6d CSPL-4027: Allow custom labels on helm chart service and service account (#1595)
82a57bed Prerelease workflow: helm charts and makefile updates (#1588)
bfc4599d Merge pull request #1580 from splunk/CSPL-3921-add-jbuczak
12d7873d Pipeline and documentation fixes (#1575)
d8cbfb06 Update ubi8 minimal version for security updates (#1571)
a3737ba3 Merge main to develop 3.0.0 (#1586)
b4d3b627 Merge pull request #1581 from splunk/CSPL-4008/docs-update-with-shc-deployer-resources
449cf66d CSPL-4008 Fix typo in Search Head Deployer resource section
9c836cbd CSPL-4008 Add deployerResourceSpec to docs
eba37bf5 CSPL-3921 Add Jakub Buczak as a maintainer in documentation and Helm chart files
c344091b Splunk Operator 3.0.0 release (#1572)
3c135bf3 Fix kustomization templates after removing kube-rbac-proxy (#1570)
5a00de07 helm changes for 3.0.0 release (#1566)
0f649747 Remove kube-rbac-proxy references and implement upgrade-sdk 1.38 changes (#1565)
9bf1baad Default Build to multi-platform amd64 and arm64 (#1557)
e5c2fd1a CSPL-3707: Update documentation around minimum number of indexer cluster peers (#1558)
4cd4cc67 Merge Splunk10 feature changes into develop (#1559)
c79fdaa9 Update Helm.md (#1563)
c9b87db7 Update of shc upgrade process (#1547)
36c1b49e Fix remote-tracking of develop to main
5360ee3c Merge remote-tracking branch 'origin/main' into bugfix/history-git-2
3b820b3f Document skipping apply cluster-bundle on cluster managers (#1556)
09dae507 Promote Develop to main for Splunk Operator Release 2.8.1 (#1542) (#1553)
e0927084 CSPL-3913: Pass dynamic environment variables in Splunk StatefulSet for Ansible-based config generation (#1555)
edb99d5d [CSPL-3912] Allow Custom Probe Scripts (#1549)
1c7adcd1 CSPL-3905: Security and dependency updates (#1551)
7a4bd216 CSPL-3867: SHC and CM Error Message Visibility (#1548)
f095f4dc CSPL-3898 Fixing int-helm tests failing after SDK upgrade (#1544)
32fcae0c Backport main to develop for Splunk Operator Release 2.8.1 (#1542) (#1543)
01f0cf6c update SmartStore documentation for gcp and azure (#1541)
3f5cd598 Merge pull request #1536 from splunk/CSPL-3851-nginx-blank-page
db2c02a5 Revert "Remove in progress phase"
3c919d64 Remove in progress phase
349ce02d CSPL-3851 Adding info to docs about session stickiness for ingress
2640e219 Remove kube rbac proxy from helm charts (#1531)
1871d25a Add more logs around invalid phase and downloadPending (#1528)
f346d144 CSPL-3783: Update AppFramework docs with troubleshooting information (#1527)
2a0682b5 Upgrade golang.org/x/net version to v0.38.0 (#1530)
e9c20923 Merge pull request #1520 from splunk/CSPL-3759-ginkgo-upgrade
9e9b018c Add DeepWiki badge (#1529)
2547df6b CSPL_3759 Ginkgo types to v2
9ee5ed65 Merge branch 'develop' into CSPL-3759-ginkgo-upgrade
6c4484db CSPL-3675 Update Operator-SDK to v1.39 (#1488)
f3a0ea9a CSPL-3784: Update base image to latest ubi8-minimal version (#1525)
384ed591 Merge pull request #1521 from splunk/CSPL-3768-graviton-pipelines-enhancements
bfb557f3 CSPL-3759 Addressing soon to be deprecated
68a92216 CSPL-3768 Addressing comments
8311e53e CSPL-3768 Addressing Copilot suggestions
f6ff895f CSPL-3768 Fixes
ae25c18c CSPL-3768 Fixes
6a0c075b CSPL-3678 Introducing pipeline for Graviton and some fixes
176200fa CSPL-3768 Not disclosing ECR secret value
57efee29 CSPL-3768 Adding inputs to Graviton pipelines and tests
0da205f7 Ginkgo upgrade
a9a1a336 Merge pull request #1518 from splunk/CSPL-3702-fix-script-variables
2fd17e57 CSPL-3702 Replacing () with {} in script variables
351393ff Merge pull request #1517 from splunk/remove-appframeworktests-from-graviton
09ea07cf Removing App Framework tests for C3 and M4 on ARM builds
980a83a2 feature: add support for pre-created PVs - admin-managed-pv annotation (#1509)
779c3a44 CSPL-3584: Split run-tests.sh into multiple files (#1507)
618cc114 CSPL-3688: Update Prerelease Workflow (#1502)
3eea678e CSPL-3186: Upgrade Enterprise Security Version 8.0.2 (#1425)
3bb95c77 set imagePullPolicy default in helm chart (#1513)
3b863ea6 clean-up deprecated dirs - .circleci & .devcontainer (#1499)
42f9b0a7 Merge pull request #1508 from splunk/bugfix/CSPL-3705-configmap-apply-idxc
470d599e CSPL-3705 Addressing a comment
d605678c CSPL-3705 Removing branch from integ tests
ed5b41eb CSPL-3705 Ignoring an error if decommisioning already enabled
f547e60d test
4f03ca8a Merge pull request #1503 from splunk/CSPL-3704_smartstore_secret_deletion
c351ba03 CSPL-3704 Remove branch from int tests
4d604e2d Merge pull request #1504 from splunk/main
044f5816 CSPL-3704 Fix failing tests
cc0350b4 CSPL-3704 Integration tests enabled to check the PR
dd7f2fea CSPL-3704 SmartStore ownerReferences removed
