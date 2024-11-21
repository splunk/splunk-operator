07714c7d [CSPL-2699]: adding Azure and GCP bucket access using sdk (#1340)
02c0cf33 Merge pull request #1403 from splunk/CSPL_3149
da70078f Use block
977bd788 Address openshift documentation
44207b23 Merge pull request #1399 from splunk/CSPL_3060
801d2d69 Merge branch 'develop' into CSPL_3060
631624ba Remove from int test workflow
e2e95412 Remove from int test workflow
213b9a44 Merge pull request #1398 from splunk/CSPL_3085
8fa1d83d remove xcontext
d94fb72d Merge branch 'develop' into CSPL_3060
ee7df2dc Merge branch 'CSPL_3060' of github.com:splunk/splunk-operator into CSPL_3060
b85e3fba Comment out one TC.
cfd03c62 Merge branch 'develop' into CSPL_3085
89c4da44 Merge pull request #1400 from splunk/CSPL_3063_helm_sva
0bfe4f96 Merge branch 'develop' into CSPL_3060
f27e9e07 Merge branch 'develop' into CSPL_3063_helm_sva
5983f25f Fix typo
08d20226 Merge branch 'develop' into CSPL_3085
7127f607 Clean Up AWS Resources After Cluster Deletion (#1396)
2850e3f8 Run only managermc
d6406bed Introduce corresponding kuttl yaml
6ea39642 Create a new folder for helm SVA testing only
c49e711d add comment for ubi tag version
5837b68f Trigger int tests
7be7f2c1 Avoid upgrade path during enterprise deployments creation.
ce39f0ca Use sha instead of label for docker build
d6ccab97 Merge pull request #1394 from splunk/CSPL_2887
0c8cbedf Merge branch 'develop' into CSPL_2887
e86436e4 Remove Makefile change
e6979eca Change def storage class
2b0ae6f3 Update K8s version, eksctl version
2a3e40c2 Merge pull request #1391 from splunk/CSPL_2887
42eef6a9 Change eks version to 1.31
f7b7e970 GITHUB-1124: Add Support for Configuring Custom Cluster Domain in Helm Chart for Splunk Operator (#1376)
1b1a3856 Merge pull request #1385 from splunk/CSPL_2823
da71f090 Merge branch 'develop' into CSPL_2823
8393b3c4 Merge pull request #1370 from splunk/CSPL_2756
e1718cde Change log
cc4fa4cd Change log
f9d1b6ab Merge branch 'develop' into CSPL_2756
f3fb07de Addresses PR#1372
c0ec26ab Merge pull request #1383 from splunk/main
219d89c7 Fixes PR#1377
fe0597ee Merge pull request #1381 from splunk/promote-develop-to-main-2.6.1
b41a3389 Docker file changes
e39df60b Generated bundle changes
bebafce6 Merge pull request #1368 from splunk/release/2.6.1
cb50ad30 Test everything together
41ea5a0d Change oldImage
6a4512a9 Change TC newImage to splunk/splunk:latest
8984bf5b Re-trigger after changing image to 9.3.0
261a6a08 Comment out redhat registry login
20d9c7c6 Try only TC2 without other suites
51e7728a TC1, TC3, TC4, TC5
94acf897 TC2
099ea8d7 Run TC1
d3c70413 Comment out TC as it is passing separately.
a5bcd2f7 Increase timeout
b84ea69e Run all tests, increase timeout
6ce8d137 Run with 9.3.0
69df2f84 Run only the failing tc
639b86a8 Make it fail again
a6dcb9d0 Try 9.2.2 in .env as well.
c98a8816 Change upload-artifacts version
44a7d3cc Re-use 0.16.1. Test out 9.2.2 image change revert
8c59c68b Test controller version 0.14.0
b76e08c9 Re-ran "make all" , "make generate"
e1875945 Re-trigger
cf64a8b6 Re-trigger
ed3676d8 Run only smoke
eff8ff08 Re-trigger pipelines
8280f038 Add PDB documentation
895e2486 Remove the helm test workflow.
056dcfd4 Try 1.23.0 and change Dockerfile to use 1.23.0
8a358414 Update controller-gen tools version for go compatibility
23744155 Shift to 1.22 to try and avoid seg fault
bb4268bb Update go version in workflows
8023e865 Update go, go-restful to address vulnerabilities
f02b69cc Update changelog.md, kick off tests
11d79626 [create-pull-request] automated change
8222cd4c Merge pull request #1367 from splunk/pre-release-workflow-update
5e34c3d5 Merge branch 'develop' into pre-release-workflow-update
76600ca3 Update gha-find-replace version, helm chart update
c92bcc42 Merge pull request #1366 from splunk/pre-release-workflow-update
f13ace0a Update pre-release workflow
cb50cba8 Merge pull request #1355 from splunk/mc-upgrade-path
7c148c26 Remove branch from int test workflow
18270e20 Re-trigger removing Xcontext
3197b7cb Remove branch from int test workflow
becf2fd1 Merge branch 'develop' into mc-upgrade-path
edd339b4 Re-try test.
6af91b54 Merge pull request #1364 from splunk/CSPL_2655_telapp
85e83c6c Merge branch 'develop' into mc-upgrade-path
37564deb Add default.meta in the telemetry addressing vulnerability
6cf2e17e Merge pull request #1363 from splunk/cspl_2652
5e58a714 Comment out 1 test case to see if pipelines pass
7959705a Add TLS config to minio client
44dd2a17 Minor error
15b829b7 Update documentation, comments
17696ab7 Fix UT
99e1806e Merge branch 'develop' into mc-upgrade-path
9a941654 Merge pull request #1361 from splunk/main
7c5c6606 disabled mc unit test case for now
9d6127f8 fixed upgrade mc test case
d7ac3398 go mod and go sum
0fd0b80f order changed for mc
