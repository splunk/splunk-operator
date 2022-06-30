df565509 Call fails when there are any Warning message from PodExec Call (#830)
3eaa2130 CSPL-1769(revised) - Fixing github issue #762 after reverting PR781 and PR794 (#829)
a522548b CSPL-1841 - Revert PR781 and PR794 to avoid nomenclauture change to PVCs(unsupported by K8s) (#828)
bf6dc817 update docs about community support for SOK (#824)
5490a5bc underscore in the metadata:name are not supported by K8s (#799)
aa2c44c7 Cspl 1635 develop (#813)
2dd0822e CSPL-1821 (#814)
1be59296 CSPL-1830 udpated nightly workflow (#817)
ce45087d Improved logging for pod reset util (#821)
6c64cc56 Merge pull request #820 from splunk/persistent-volume
fb466567 fixed an issue in support PVC in appframework
cde86b82 CSPL 1808/1809- Threat model Doc Changes (#810)
fa580374 Merge pull request #818 from splunk/cspl-1813
36d4f880 Merge branch 'develop' into cspl-1813
1362e518 adding run as nonroot
beea11e2 Merge pull request #816 from splunk/CSPL-1754-2
1e2ca682 based on threat model recommendation
0d667fad CSPL-1754
b7c67a7b Merge pull request #811 from splunk/dev_cspl_1752
b585d199 CSPL-1752: Do not update status on cached CR
641f62de Enhance nomenclature for storage config code (#794)
a13b161b CSPL-1787 - Updated docs to add scc nonroot to splunk-operator-controller-manager in openshift environments (#791)
5c870c57 Merge pull request #806 from splunk/feature-appframework
8268b1f0 removed branch from integration testing
ee6b8d76 adding PVC changes based on PR review
668f5cde addressed Gaurav comments (#808)
5c642f25 Cspl-1728: document changes for 2.0.0 (#807)
d960daa0 CSPL-1754 (#802)
fb4b7e9f Merge branch 'feature-appframework' into cspl-1740
65a074ff CSPL-1598: Delete apps from app directory while app installation is in progress (#800)
c49085fd CSPL-1740: merge develop into appframework (#797)
967c3fa1 Merge branch 'cspl-1740' of https://github.com/splunk/splunk-operator into cspl-1740
c4ad4b53 Fix failing test case due to early pod reset check
5b031dcf Merge branch 'cspl-1740' of https://github.com/splunk/splunk-operator into cspl-1740
cebf7b04 fixed smartstore ut
ddab75c1 make file changes from PR
42662f30 updated with review comments
3f8c087a fixed issue in clair scanner makefile
b1b5a510 merge issues fixed for git workflow
da2c198a fixed run test call
fc356df8 reverted back few changes - merge conflict issues
f9d82743 changed bought by feature-appframework
100bde66 Merge branch 'feature-appframework' into cspl-1740
bac632f6 added Secret and Configmap to CR Update call
17f76506 changed sleep time to 1 microsecond
916488a2 format issue fixed
3f0fb1b7 enhanced update CR call in test case
0c4766cf fixing few issues found after develop changes in test cases
46eebbcf fixed app framework mounth path in test case
54b127ff fixed app framework mount path
b09aae3a fixed app framework mounth path in test case
06640fd7 moved app framework mount point to /tmp for unit test case
8a8fe21a commenting one unit test case for now
805d90e2 adding more changes to fix
529a8da9 running integration testing
6f4b965e Merge remote-tracking branch 'origin/develop' into cspl-1740
7f89ebad CSPL-1801 - Avoid odd number of arguments passed for logging (#796)
fd1a933e [CSPL-1735] integration workflow changes with git checkin image to appframework (#792)
a9272d40 CSPL-1600 (#789)
e6c314f5 CSPL-1769 - Change the naming of volumeMounts to adopt setup of init container (#781)
e1e69dec CSPL-1727: Manifest files to differentiate namespace scoped and cluster scoped (#779)
99a0a5c1 For removing  high vulnerability CVE-2022-21698 (#788)
029ad3d5 MC Coverage (#784)
27d9ca49 installing secruity patches for ubi images (#780)
cf066dae CSPL-1770: Always update the last LastAppInfoCheckTime (#778)
a77c0be6 CSPL-1596: Update apps on app source while installation is going on (#757)
568e8d0f CSPL-1625: App framework unit test (#745)
9d395080 CSPL-1601 (#752)
c8caa7a6 CSPL- 1768 - Adding an annotation to define a default container (#777)
385ab864 Fixing broken link for nginx ingress controllers (#776)
c3478128 Doc bug (#775)
8710b0d8 fixing a log format that is causing the panic (#770)
bdc6f709 Adding debug logging for secret management (#767)
527e3b36 Remove remotepath docs example in defaults (#766)
4bfd9a8e CSPL-1749 - ImagePullSecrets config docs along with other common splunk spec para… (#760)
51114a21 CSPL-1763 - Fixing github issue for logging error unnecessarily (#755)
9d53c258 CSPL-1757: Correct the log key value pairs (#768)
68aa42fa CSPL-1729: Detect and update the init container image (#753)
7fc723ea CSPL-1723: Updated Release Workflow (#759)
35605850 CSPL-1746: Cleaned up duplicate code in testenv.go (#758)
299331c6 Fixed nightly test log collection on failure (#754)
b75feb1c CSPL-1662 - Adding github issue forms. (#721)
285baed4 CSPL-1746: Cleaned up duplicate code in testenv.go (#751)
bbffe851 CSPL 1218: App framework authentication tests (#744)
d8070e41 Updated code to get correct name for s3 secret name (#750)
6b5a52a8 CSPL-1589 (#737)
f17c7039 CSPL-1725: fixed splunk image tag (#749)
57a0b328 Remove spec.Mock from MC code (#733)
525cd6e7 CSPL-1707 - Allow configuration of ImagePullSecrets (#730)
bd4081bf Pass correct parameters to setupInitContainer in clustermaster.go (#746)
7a67b6e2 Enabled int test on feature appframework (#747)
078f8181 rebase changes with PR:707 (#727)
781e3428 Removed PVC cleanup to make cluster-down (#741) (#743)
7dd18e93 CSPL-1712: Enterprise common API changes (#723)
beda8ba6 Minor doc changes after 1.1.0 release (#742)
bba64a86 Removed PVC cleanup to make cluster-down (#741)
810739b0 Merge pull request #740 from splunk/master
93edb2ca Merge pull request #738 from splunk/master-doc-changes
67a4889a splunk image changed in the documenation
1ee8e41b few more formatting issues fixed
0b9736ab Fixed formatting and version of splunk docker image
eb674f88 Merge pull request #736 from splunk/master-doc-changes
9a78c37c Merge branch 'master' into master-doc-changes
c6a202b3 fixed formatting issues
3e49be75 fixed spelling mistakes
d683c4a4 added PR review changes
ddddc1a6 updated document changes for upgrade scenario
06111874 updated document changes for upgrade scenario
fbe14d05 CSPL-1599: Test cases to verify feature disable (#716)
14a9b0e1 Sdk upgrade changes for appframework (#707)
5a89ce42 CSPL-1664 improve cluster down command (#715)
03a3a817 CSPL-1593: Add test where operator is reset while app install is in progress (#701)
dff1e6a1 CSPL-1650: Added wait for splunk pods from previous runs to be cleaned up before running test cases. (#686)
178a36dc CSPL-1670: Add region as a configurable parameter in volume spec (#700)
beefe705 CSPL-1661: Config options for the Phase try count and yield timer (#692)
64b0d0e9 CSPL-1595: Add New apps to app source while installation is going on (#693)
94cbd56d CSPL-1656: Merge branch 'develop' into feature-appframework (#694)
96fd44ce CSPL-1666 (#696)
7c8c1192 CSPL-1642: fix a race condition due to which int tests were failing (#680)
5275919b CSPL-1597: Multiple standalone in same namespace (#687)
c153ae6f CSPL-1633 (#690)
381e8b54 CSPL-1633: Implement common code refactoring studied in CSPL-1580 (#679)
9cd498a3 CSPL-1604: Update config map for SHC and CM separately to avoid race condition (#674)
cb15bfed CSPL-1326: Update the File permissions for the cluster scoped apps (#682)
e47fe115 CSPL-1639: Fix a bug where streamoptions needs to be reset for running same command on pods (#677)
3be8fb46 CSPL-1638-disable-failing-test-with-jira-support-for-logs-archive-on-… (#675)
62042677 CSPL-1552: Update PodExecClient and MockPodExecClient to be used across the code (#662)
be24d2b7 CSPL-1579: Temp AppFramework download location on Operator pod (#672)
15565ad6 CSPL-1460: Phase-2 Code cleanup - Remove app listing configMap and init-container creation (#671)
653dec3a CSPL-1620 (#670)
671f8f5b CSPL-1539: Manual Poll test case when C3 & M4 are configured with both cluster and local install (#666)
015a6c24 CSPL-1591: Cluster scope: avoid unnecessary scheduling of the podcopy workers (#665)
bf552636 Modified functions pointing to old code (#664)
3f045199 Add phase change logic to replace aggressive wait (#661)
88bc49e7 CSPL-1494: Some misc improvements to AFW scheduler code (#658)
ec41dde8 CSPL-1574-refactore-get-pod-life-function (#659)
a91e5249 CSPL-794: Update AppFramework.md (#642)
0f5076b7 CSPL: Test cases for c3 and m4 appframework with local scope and manual polling (#656)
ff04d8ab CSPL-1546: Added pod reset check to C3 and M4 test cases (#643) (#641)
feec32c7 CSPL-1546: Added pod reset check to C3 and M4 test cases (#643)
dfc8cb99 CSPL-1329: Migrate the Phase-2 App context status to Phase-3 (#635)
f1a492c9 CSPL-1529: Fix a bug where ClusterMaster was not watching for changes in configMap (#640)
ae2536f3 Merge pull request #636 from splunk/CSPL-1550
f9470bf8 CSPL:1170 Check for pod reset on S1 due to app install via AppFramework (#632)
30a05b01 CSPL-1169: Apps should be correctly installed on standalone pods when scaling up/down (#634)
f6a71496 CSPL-1550: Merge develop to app framework feature branch
014fd164 CSPL:1527 C3 test cases with both local and cluster install (#617)
8a44088a CSPL-1449: Install worker handler to support multiple app installs (#609)
c32b3400 CSPL-1328: Handle long running SHC budle push (#615)
dd28804a CSPL-1278: Log additional debug information after a functional test fails (#613) (#619)
2f942e4f Workaround for CSPL-1532 (#616)
f62b82cf CSPL-1333: Implement playbook for SHC (#607)
8ccfe521 CSPL-1469-added phase 3 for S1, C3 and M4 SVA. (#606)
e763be20 CSPL-1507: App Framework scheduler should be unique/CR (#610)
f1266600 CSPL-1512 (#605)
ce462582 CSPL-1418: Use common utility to create appframework spec for S1 (#604)
2c62e74a CSPL-1332: Cluster scoped playbook impementation for indexer cluster (#590)
9f21dc15 CSPL:1417 Added different app source per CR (#573) (#577) (#602)
8111fa3b CSPL-1452: Backport CSPL-1417 to feature-appframework (#598)
f31bd576 CSPL-1477 (#595)
16001e80 CSPL-1331: API for local scoped app pkg install (#585)
8a94e737 Fix M4 Test case issues (#588) (#591)
1610351f CSPL-1454: Fix s1 appframework test (#582) (#587)
21516796 CSPL-1443: Remove references to old MCPodReady function. (#586) (#589)
400bf238 Merge develop to app framework feature branch (#578)
77bb843c CSPL-1345-test changes for app framework phase 3 downlaod api (#574)
cda39b0f CSPL-1340: Copy the app package from the Operator Pod to the Splunk Pods of the CR (#561)
27e487bf CSPL-1384: Parallel downloads with app framework scheduler (#563)
e014065c CSPL-1337: Scheduler pipeline for App Framework (#545)
2bde6b49 CSPL-1363: Added UTs for parallel app downloads (#544)
9ff317ae CSPL-1334: Added framework for downloading multiple apps in parallel on operator pod (#527)
c815e9ad CSPL-1291/CSPL-1336: Merge develop to feature branch  (#518)
9ed2c511 cspl-1256: API for the file copy from Operator Pod to any other Pod from a custom resource (#515)
3fd82baa CSPL- 1254: Added DownloadApp API for AWS and MINIO client (#499)
0f3f74f8 CSPL-1235: Manual poll (#480)
5d450290 CSPL-1267: Merge develop to app framework (#486)
00df9b42 CSPL-1221: Manual trigger for app update (#468)
027cb93f Merge branch 'develop' into feature-appframework
ff1946ca Merge branch 'develop' into feature-appframework
ee75ba17 CSPL-1201: Added validation checks for invalid storage type and provider (#435)
