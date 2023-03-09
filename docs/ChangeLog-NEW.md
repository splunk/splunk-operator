c381f12f fixed pre release workflow code (#1111)
6f997a6e Bump Azure/setup-kubectl from 1 to 3 in /.github/workflows (#1106)
a1b36679 Cspl 2322: FOSSA Scan fix (#1109)
82186133 cspl-2319: fix for host path volume in splunk operator helm chart (#1108)
ef215b13 Added symlink int test and fix for Cluster Master CRD (#1096)
c87eec1c CSPL-2317: Helm test issues (#1098)
e272252c Cspl-2314: support for service template in helm chart (#1097)
1f0e9b1f Cspl 2318: adding container name to pod exec command (#1100)
87a6b4d9 Merge pull request #1101 from splunk/fix-image-build-errors
e8353613 Updated go.sum
c84fb62e Merge pull request #1099 from splunk/build-issues
0ea9a73f build issues fixed
baef54e7 CSPL-2299: Ginkgo v2 (#1074)
49cf86dd cspl-2295: support for topologySpreadConstraints in splunk pod (#1072)
d460c0e4 CSPL-2307 - Modify logging for probes (#1094)
3763dd31 Cspl-2303: 1086 changes to develop branch (#1087)
f6167ca4 Develop to main doc change (#1056) (#1090)
cd8c9c1d Service name conflict for ClusterManager/IDXC names (#1073)
94e002b2 CSPL-2301: fix for security patch: fix CVE-2022-27191 (#1081)
253e591b cspl-2294: support custom volume creation in splunk-operator (#1071)
12b3089f cspl-2293: removed goclient dependency for premetheus (#1070)
01f3f36a Cspl 2291: helm test case to test smartstore with standalone (#1068)
a6fac4c1 helm: test case to test extraManifests (#1055)
a76ca799 Fix MC Bug, how MC finds if the CR exists in the MC configmap (#1043)
1c8471ec solves issue #1063 (#1064)
1212de5a chore: allow creation of additional arbitrary k8s manifests (#1005)
b205975c Fix logging crash (#1057)
984c6f8d CSPL-2255, 2256 - Fix smartstore symbolic links, add validation check for phase info (#1053)
feb76422 Develop to main doc change (#1056) (#1058)
1e42654b Fix documentation which indicated ES being unsupported (#1054)
d9abf449 Fix the splunk image switch on main branch (#1050)
356ade32 Merge pull request #1049 from splunk/develop-doc-changs
ca2fa78f k8s supported version updated to 1.20+
e523a5da Merge pull request #1047 from splunk/main
3e18e164 Merge pull request #1045 from splunk/promote-develop-to-main-2.2.0
4d15fe0d adding pipeline change to helm to run release
1c3cfe5a resolve merge conflicts
a0f64774 resolve merge conflicts
42bbd679 cspl-2253: indexer deletion in upgrade scenario (#1041)
6945df7f fix Storage Config issues for LicenseManager (#1040)
f6dc3e70 Splunk Operator 2.2.0 release (#1028)
47218929 cspl-2220: adding helm chart changes for version support (#1031)
49cac823 Fix for ES failure on pipeline (#1036)
66ed931e Updated checkout action to pull develop branch for image creation (#1038)
1e1d6e9e Merge pull request #1035 from splunk/cspl_2242_probes
970271a2 Fix faulty default FailureTreshold values
c960c03b Update docs for BA service not supported with ES (#1026)
8a4298d7 Merge pull request #1029 from splunk/CSPL-2223
9ad3a702 CSPL-2223
94d56cbf Merge pull request #1018 from splunk/feature/es_appfw
8045af5a cspl-2205: helm chart support for es appframework feature (#1020)
3d9c05b5 Merge pull request #1023 from splunk/cspl_2211_gke_test_failures
91049390 Merge pull request #1022 from splunk/CSPL-2206-doc-changes
c45cf52b Marking phase info before deleting app package on operator as delete checks for phase info
1e652c4a review splunk version and remove extraEnv for web.conf push
b1534c8a CSPL-1583: Update docs for ES app installation (#1011)
ff62c546 Merge branch 'develop' into feature/es_appfw
bf989f09 cspl-2204: copied operator chart to enterprise dependencies (#1017)
f3f1904e Remove branch from prod sec workflow
9cc70b88 Add branch to prodsec workflow
73c5962f Merge branch 'develop' into feature/es_appfw
64a28887 Merge pull request #1010 from splunk/revert-1006-cspl_2187
12a77817 Revert "CSPL-2187 - Make ssl_enablement ignore as default, disable strict for shc (#1006)"
e0e6c47d Cspl 2114: Add tests for Install of Tech addon app on Indexer (#992)
638e65be CSPL-2187 - Make ssl_enablement ignore as default, disable strict for shc (#1006)
02165b85 CSPL-2178: es post install failure should not trigger update of app install (#998)
b863cf4b CSPL-2165 - Static analysis bug fixes for staticcheck against operator code base (#984)
5d2a9a7b Merge pull request #1000 from splunk/merge-develop
a0f7e942 Merge branch 'main' into merge-develop
4e1e0eae Splunk Operator 2.1.1 release (#999)
dbe8624f cspl-2177: added dockefile changes to fix CVE-2022-42898 vulnerability  (#996)
d53592cc CSPL-2172 - Fix es install fail scenario (#993)
90c456e7 Merge pull request #988 from splunk/dev2es_merge
508cc4d3 CSPL-2167 - Make strict flag as default flag for essinstall (#991)
e2a78dca Merge remote-tracking branch 'origin/develop' into dev2es_merge
2dac9c47 Initial es_App changes (#985)
f404674c remove dev2es_merge for prodsec scan
604fa53c Merge pull request #990 from splunk/CSPL-2168-suppress_know_msg
44cfb5a9 add dev2es_merge for prodsec scan
1e939759 suppress known harmless message
65d1d9a7 merge dev to es branch
1cf01902 merge dev to es branch
ee0c377a Changes for enabling ES app on operator using appFramework for standalone and SHC (#969)
902f4cee Merge pull request #967 from splunk/CSPL-2112
b9399780 CSPL-2112 Initial changes for ES app
5709aecd Add CRD spec for ES app installation epic (#958)
