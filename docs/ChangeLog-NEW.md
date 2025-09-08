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
