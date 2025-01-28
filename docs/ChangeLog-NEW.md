a24dec29 CSPL-3064: Support for Distroless Image Creation in Splunk Operator for Kubernetes (#1421)
95985143 CSPL-2966: Feature: Manual App Updates per Custom Resource (CR) in Splunk Operator (#1395)
07f14471 CSPL-3156: Add kubectl-splunk Plugin for Executing Splunk Commands within Kubernetes Pods (#1407)
59b81710 Merge pull request #1419 from splunk/CSPL_3256
3d5dd4b4 Restore int-test workflow
10f30f91 Merge branch 'develop' into CSPL_3256
4d4f6a90 [CSPL-3269] Change error log for clarification (#1422)
1138d9db Add node affinity as well.
9f6642c7 Merge branch 'develop' into CSPL_3256
8306aea3 Merge pull request #1417 from splunk/feature/CSPL-3253
987c4cf6 Merge branch 'develop' into feature/CSPL-3253
1696ce36 Merge pull request #1420 from splunk/feature/CSPL_3298
478244ad Add UT and return error if not deployer sts
502397de Add a comment, rename TC.
9b3a1f30 remove specific branch to run integration tests
75ded11b Fix int test bug
713d14eb Trigger int testing again
98743b4a Merge branch 'develop' into CSPL_3256
e8190128 Remove change splunk operator name step in integration test workflow
79d28de5 Remove SHC updating phase check
21b12b23 Merge branch 'develop' into feature/CSPL-3253
5fa3bd29 Merge pull request #1393 from splunk/CSPL_2920
9fc919f1 Integration testing enabled
37b23749 Initial changes to support deployer spec in SHC CRD
261d84c7 add back feature branch for integration test workflow trigger
22fba1a1 feat: [CSPL-3253]: Change default storageClassName value in PVC
5749ef8e re-enable test case, correct merge conflict
85acc39f merge commit for pulling splunk enterprise image
ddbb6b10 merge integration test fixes branch
4b50e139 clean up new workflows
f620d4b5 dump splunk version during consistently check for search head cluster
2d1f4a01 add sleep for managermc1 failing test case
b116765f get correct standalone for readiness checks
68cf212a get correct standalone for readiness checks
2bee1d03 separate suite tag for failing test
44a01d42 trigger rebuild for sok container on ubuntu arm64
501dea21 remove build and test workflow for now
223a9573 trigger rebuild for sok container on linux arm64
f70f23d8 trigger rebuild of sok images for arm64 architectures
3a78dfa9 trigger integration test for PR
fc95d7cc use shorter label for testing tag
8d4568b4 use new label to test app framework tests that hang during teardown
012d5c14 Merge branch 'develop' into CSPL_2920
0e066ac0 Merge pull request #1414 from splunk/main
6270cdd6 trigger AL2023 build for splunk 9.2.4
98df5dd0 trigger Ubuntu build for splunk 9.3.2
a02f50d7 trigger Ubuntu build for splunk 9.2.4
a625571d trigger AL2023 build for splunk 9.3.2
2a07a314 trigger AL2023 build for splunk 9.2.4
35f3e15c Merge branch 'develop' into CSPL_2920
44e54e0e Trigger workflows for 9.2.4 AL2023 ARM64
da1f11a5 Merge branch 'develop' into CSPL_2920
6685ec54 Merge branch 'develop' into CSPL_2920
0bab91fc Merge branch 'develop' into CSPL_2920
354eefea Trigger both arm and ubuntu. Add cert for ubuntu
81e75026 Try installing certificates on SOK container
8ab8c1a3 Merge branch 'develop' into CSPL_2920
31fa22fa Merge branch 'CSPL_2920' of github.com:splunk/splunk-operator into CSPL_2920
a8881979 Trigger for 9.2.4 AL2023 ARM
524d5e69 Merge branch 'develop' into CSPL_2920
568290b9 Change to AS
97c5bec4 Remove space
5187f089 Build for amd64 as well for pipelines
458b751e Fix unattended-upgrades
2c618525 Run without package versions
4ba29e4b Test package version
33f3aba4 Change logic for Ubuntu
6c9b7899 Fix docker builds
e56a203a Address review comments
87fd60c8 Echo BASE_OS
b570cc7e Pass as build arg
d2d124c7 Add support for Ubuntu
aa6ec96d Avoid vul testing for graviton for now
945e1495 Disable int tests for now.
f2de2397 Re-trigger
991d80e1 Trigger
879ed335 Re-trigger
11d1d3f9 Set graviton to true int tests
133bec49 Pull image fix - int tests
8d9dfdb5 Trigger int and smoke as well
1555d216 Don't use platform in FROM in dockerfile, remove TARGETOSIMAGE, ignore int tests for now
a5d7225b Further enhance
45ea9da0 Update error logs
789e4788 Enable int tests
a18b2e77 Re-run tests
456d86aa Remove push-latest, re-run pipelines
dd88888c Enable everything and try again
ec29b769 Avoid describe
6aafeb70 Re-run change kust
9be9090b Don't need to tag for graviton
a3c19087 Describe
3e7345f6 Dump version
12531e77 Change eks instance type
bc3e6611 Display operator image
a391b10b Don't push for graviton
40d8f66f Pull locally
34379dc3 Change tag
36ac76c7 Hardcode
3d9e4e7d Add a default value
3f745cf4 Try passing build arguments
f7a2648f Add the argument again
dfd08766 Try this
b3f836df Merge branch 'develop' into CSPL_2920
b3eb55f5 Initial changes for graviton smoke tests
68e49c45 Minimize changes only to smoke tests to start with
f2fc5f8b Trigger int testing
f6bef931 Change env variable value
05400a3e Test again
52ce30c1 Add a '.'
1ae6f15a Use docker-buildx and make smoke tests run
