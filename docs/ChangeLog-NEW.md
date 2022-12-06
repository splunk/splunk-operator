dbe8624f cspl-2177: added dockefile changes to fix CVE-2022-42898 vulnerability  (#996)
e3573a4c CSPL-2151 - Changes to delete owner references of statefulSet on CR deletion, check for stale references to CM (#983)
69246e9b CSPL-2156 - Add Static analysis against code (#977)
5b35546d Merge pull request #981 from splunk/helm-workflow
41badb74 adding helm test to main branch
993c1698 change helm m4 test to use zone 2b and 2c (#980)
82d27082 Merge pull request #978 from splunk/update-make-generate-step
f1deb4f5 Add workflow to run additional push bundle steps
de499e2a Merge pull request #976 from splunk/cspl-2149-new
9bc6fde0 Merge branch 'develop' into cspl-2149-new
3a2c2ff3 rolling restart in reverse order
0c84acf2 Merge pull request #975 from splunk/cspl-2149-new
992e6778 set maintenance mode added validation
d043db5e Merge pull request #973 from splunk/cspl-2149
0217fd7c Merge branch 'develop' into cspl-2149
8e06c41e cspl-2149: fixed error in json formatting, tested
ed233b39 Merge pull request #972 from splunk/cspl-2149
b9c33dc1 cspl-2149: added job with node affinity to cluster manager
d01becbb Merge pull request #970 from splunk/update-image-docs
f6cb292a Update splunk Image in docs
117c5b94 fix flaky test on standalone (#965)
50406b5d Merge pull request #966 from splunk/cspl-2136
6b7ee8ce removed enterprise internal license
48c2f455 Merge pull request #964 from splunk/resolve-failure-in-develop-to-main-workflow
a71bb79a Fixed workflow issues
e9bff0c4 Update operator upgrade commands (#962)
6673774e CSPL-2126: Fixed probe updates (#959)
a4c7c377 CSPL-2125: operator-sdk and golang upgrade (#952)
4dc3a5ef Edit and remove redudundant cases (#948)
03cb063a CSPL-1406: Master to main changes (#949)
91164bed Splunk Operator 2.1.0 release (#943)
7d9e18f9 Merge pull request #946 from splunk/CSPL-2109-liveness-improvement-perf
53329d16 fine tune command to get splunkd pid
bff07b54 Merge pull request #944 from splunk/fix-reviewer-list
9106bc91 Update revier list
73be9abe CSPL-2099: Address prod sec inputs for azure blob authorization (#939)
fe88c555 CSPL-2102: Select tests to run on Azure to avoid reaching time limit on Github actions (#936)
5d6bb326 fix master suite test cases to use masterSpec (#940)
d08df4fa Merge pull request #942 from splunk/fix_pre_release_workflow
92272d20 add install operator-sdk step to the pre-release workflow
66c05e6c Merge pull request #941 from splunk/eks-cluster-k8-version-configurable
83f03d4c Configure k8 version of EKS Cluster
232e2fb3 Merge pull request #937 from splunk/upwardmerge_azure_to_develop
c1f3a914 make curl work
cb1f62aa eks ctl related changes to fix build/test
30fd6018 remove helm for int and smoke workflow
fd9d2985 Merge branch 'upwardmerge_azure_to_develop' of github.com:splunk/splunk-operator into upwardmerge_azure_to_develop
39711867 update reference of older operator and splunk version to newest
fe19e42c Merge branch 'develop' into upwardmerge_azure_to_develop
2a7c7ed1 CSPL-2103: Fix int test on develop (#938)
e81227eb address review comments- remove appframework_azureblob from workflow
2c05b524 CSPL-2052-merge appframework_azureblob to develop
eda7cfb2 CSPL-1946: Run tests on Azure with managed identities (#924)
046d8980 Merge pull request #933 from splunk/fix-int-test-azureblob
fa16532d CSPL-2092: have better error message when authorization fails (#931)
a7504766 Merge pull request #934 from splunk/fix_aws_int_test
4b34a890 Fix int test on azureblob
c9b1313d fix failing int test
b0794874 Update Test to check cluster platform before running test (#932)
f91bd9c8 Merge pull request #930 from splunk/merge_probes_into_develop
eacf272a Merge pull request #928 from splunk/merge_develop_into_probes
34e9fd6e Merge branch 'develop' into merge_develop_into_probes
06eb434b Merge pull request #917 from splunk/CSPL-2078
ac4d9121 Merge branch 'develop' into CSPL-2078
6db84843 Merge pull request #927 from splunk/local_probe_ds
3a7ab25d Merge pull request #925 from splunk/CSPL-2033
32de0fcd CSPL-2085: Allow only configurable probe fields through the CR
6de955ed Merge pull request #915 from splunk/CSPL-2023
959101c2 changed sdk version
f72661b1 Final run
5a0af77b CSPL-2023: Added assertion for readiness and liveness script
b1995462 CSPL-1979 Add support for startup probe (#916)
fc0a5bc5 Merge branch 'CSPL-2033' of https://github.com/splunk/splunk-operator into CSPL-2033
776f7cd6 CSPL-2063: Update app framework doc to include azure blob usages (#920)
ba60459a removed cspl name from workflow for semgrep
19bca57b changed multisite_manager to multisite_master
2dd97ee4 Merge branch 'develop' into CSPL-2033
2f372b3d CSPL-2036 (#910)
c985c8e2 Cspl 2026 Doc Update for probes (#926)
1723945e testing out with using the same cluster
f47009af Merge branch 'CSPL-2033' of https://github.com/splunk/splunk-operator into CSPL-2033
ead68d47 fixed an issue in predicate comparisions
06c85501 testing out with using the same cluster
bc45c655 Merge pull request #922 from splunk/merge-develop-probe-feature
6bb26b4f Merge branch 'feature-readiness-liveness-configurable' into merge-develop-probe-feature
729a4ad5 Merge pull request #919 from splunk/cspl-1983-latest
e0fcf9cf Merge pull request #923 from splunk/develop_to_azureblob_merge
3fea03c7 Merge branch 'CSPL-2033' of https://github.com/splunk/splunk-operator into CSPL-2033
cffecef8 CSPL-1983: Reduce the liveness probe level during the maintenance activity
006bdcd4 Merge branch 'develop' into CSPL-2078
f1243a7b testing out with using the same cluster
5dddb0a0 Merge remote-tracking branch 'origin/develop' into develop_to_azureblob_merge
02de2f43 Merge branch 'develop' into merge-develop-probe-feature
327d0c61 Merge pull request #918 from splunk/fix-false-positive-in-int-test
937ebb73 testing out with using the same cluster
ecaa0775 Merge branch 'CSPL-2033' of https://github.com/splunk/splunk-operator into CSPL-2033
a6d86c07 Merge branch 'develop' into CSPL-2033
cf41ddd4 CSPL-2033: Update v3 to v4 for helm tests
4a6b171b Merge branch 'CSPL-2033' of https://github.com/splunk/splunk-operator into CSPL-2033
3a0fdb29 CSPL-2033: Update v3 to v4 for helm tests
d77132a6 Fixed false positive in int test
8ae127f2 Merge branch 'develop' into appframework_azureblob
2bf08344 added semgrep static sec threat model analysis fix, fixed workflow
0a2420bd added semgrep static sec threat model analysis fix
8b8028d5 added semgrep static sec threat model analysis fix
95fead50 added semgrep static sec threat model analysis fix
856f29e4 added semgrep static sec threat model analysis fix
785e098e added semgrep static sec threat model analysis fix
d1182b53 ls
28c3134d Merge pull request #908 from splunk/CSPL-2065-2066_azure_generate_crd
c40fe559 CSPL-2077: Added semgrep workflow (#912)
d080346c Merge pull request #911 from splunk/fix-issues-on-develop
6105a57a CSPL-1980: Add API to add support for K8_OPERATOR_LIVENESS_LEVEL (#906)
36561979 Fix issues identified during merge of develop branch to feature-readiness-liveness
bf4ca5e3 Merge pull request #905 from splunk/merge-develop-to-feature-readiness
3121fb2f Merge branch 'feature-readiness-liveness-configurable' into merge-develop-to-feature-readiness
69d62fff CSPL-2065, and CSPL-2066 - update common_types.go with azure blob related comments. generate the crd files. automate the crd copy to helm charts
078a6dbc Merge branch 'develop' into merge-develop-to-feature-readiness
b63934fe Resolve unit test failure
2b624522 Merge branch 'appframework_azureblob' of github.com:splunk/splunk-operator into appframework_azureblob
26fda1ac Cspl 2047 Added Fossa Scanner to build and test pipeline (#904)
10c842b5 CSPL-2018-cm-implementation-for-probes (#901)
03bd35a8 CSPL:2010 Pipeline for helm testing (#896)
4bd47cab Merge pull request #902 from splunk/merge-develop-azureblob
25689306 Merge pull request #900 from splunk/CSPL-1981
59b77002 adding review comments
c63ea4d0 adding review comments
32fec219 CSPL-1981: Add a lightweight liveness probe
b1005e8d Fix to markdown keeping table from being displayed (#895)
f0e58070 Merge branch 'develop' into merge-develop-to-feature-readiness
d30fd0b2 docs changed to point to v4
f3dfc149 added context to azure util functions
29601892 fixed logging using logf instead of fmt
10462574 merging develop branch to feature azureblob
33450bb0 Merge remote-tracking branch 'origin/develop' into merge-develop-azureblob
e7f031cd Merge branch 'appframework_azureblob' of github.com:splunk/splunk-operator into appframework_azureblob
472a0dcb CSPL-2035: updated the doc for the preferred method of smartstore configuration (#899)
38e5161f CSPL-1923: Addition of code and unittests to download the app packages (#892)
20f4630c CSPL-2034: Address log formatting errors, which is causing operator to crash (#898)
50f05229 added support matrix for 1.1.0 and 2.0.0 (#894)
cb5c065d CSPL-2022: support for v4 API in helm chart  (#890)
fbf42f78 Replace some hard coded strings with their counterparts in names.go Force use of TLS1.2 and http2 as it was in AWSS3Client (prodsec requirement)
9af3da75 remove redundant defer close
d6d6181b rebase to parent and merge
28d6ac2d CSPL-1923 - addition for downloading app packages
e77d8275 addess review comments
389079c1 Fix lint issue and address review comments
d67c31b3 Formatting changes for azure listing URL
7d2343db Add support to list azure apis with auth framework for secrets and IAM
be844fd4 CSPL-1923 - addition for downloading app packages
319a4bd5 CSPL-1987: Automation for helmcharts (#888)
94504bc4 Merge pull request #889 from splunk/CSPL-1823a
b9c5b203 CSPL-1997 (#887)
e1d7a030 CSPL-1823
6f7838cd CSPL-1935, 2005, 2004 - Implementing listing of apps for azure with associated auth framework and some refactoring  (#879)
116cf729 Adding SVA Support to Helm charts (#880)
e4404efc CSPL-2002: Update docs for Support Reference (#883)
075c2a8d Minor sentence end change.
54b5bd3e addess review comments
3d81d21f cspl-2017:removed support for alpha and beta release crd (#882)
6fb41770 Merge pull request #881 from splunk/cspl-2016
80fb59e7 Address review comment - changed the x-ms-version to a latest version per azure doc https://docs.microsoft.com/en-us/rest/api/storageservices/versioning-for-the-azure-storage-services
869b81d1 Address review comments
16c062cb Fix lint issue and address review comments
8ab960d0 cspl-2016: dockerfile with latest security patches
fd46c96e Cspl 2003- Added unit test for getProbeWithConfigUpdates (#878)
1ac1d8a5 Formatting changes for azure listing URL
1a05e2a8 Add support to list azure apis with auth framework for secrets and IAM
62306084 fix formating issues
948e32f8 CSPL-1935: replaced instances of S3 with RemoteDataClient
2f10addc CSPL-1924: add azure blob in the app frame work (#873)
d610aff2 Merge pull request #844 from splunk/feature-biaslang
137bf56b Addressing review comments
84367806 CSPL-1977-CRD support for Readiness, Startup and Liveness Probes (#876)
0d664a6a Merge branch 'develop' into feature-biaslang
31cb78c4 cspl-1896 : Helm documentation for splunk operator  (#865)
542b85f6 CSPL-1996 - Upgrade CRD API version for *Manager CRDs (v4) (#872)
4ac1abcd Merge branch 'develop' into feature-biaslang
3a0e0d96 CSPL-1945 (#874)
81302f2f CSPL-1985: Helm repo under Splunk domain (#866)
5d2db2dc Merge pull request #869 from splunk/cspl-1874
ff1f4669 Revert "CSPL-1924 - Update the app framework to allow azure blob client"
df7b98e7 CSPL-1924: Add azure blob to the current framework. Generalize s3 to remotedataclient
bf79e548 Revert "CSPL-1924 - Update the app framework to allow azure blob client"
598db462 Merge branch 'appframework_azureblob' of github.com:splunk/splunk-operator into CSPL-1924-Frameworkextension
d42092c1 CSPL-1924 - Update the app framework to allow azure blob client
4b38c0b9 adding instance support for Splunk CRs
3366416a Update bundle.Dockerfile
855803f8 Update int-test-workflow.yml
ebfc13e0 Added new Manager CRDs to Helm Chart and other adjustment post bringing develop latest changes
02843694 Merge branch 'develop' into feature-biaslang
cd5e742f Fixing int-test by using shorter test names
4610d844 Changes from PR 851 missed in merge (#862)
bb98f491 Attempt to fix int-tests using shortnames for test names
8aa6fc91 Fixing comments for Int-tests that use Master CRDs
70848032 cspl-1970: splunk operator 2.0.0 bundle support (#856)
4d8f01b9 CSPL-1862: Helm Chart for Splunk Operator (#840)
3b574ab5 Fix pipeline error (#851)
9bee3bd5 CSPL-1944 (#850)
934ccb13 CSPL-1974-add-make-cmd-for-ginkgo-golint-install (#860)
7355d0c8 Merge pull request #858 from splunk/CSPL-1922-azureblob
8a4d328a Cspl 1963 - Fix workflow issues on master branch (#853)
d389188a CSPL 778: Test standalone with Ephemeral /etc and /var storage (#852)
e25b8778 Update appframework_utils.go
e97cf00a Update lm_s1_test.go
94201086 Merging manager & master testing code
cdaa0b46 BiasLang w/ testing code:
db50832b CSPL-1922: Allow azure blob as valid provide and remote storage type
5d4ca169 Spliting cmaster tests per architecture to avoid timeouts
584cf446 Fixing tests for Telemetry app
9c86a1cb Fixed scaling int-tests
513c6798 CSPL-447 (#849)
9eceec90 Fixed integration tests for SmartStore and MC
6234f336 Temporary change to enable all tests
e129910e Updated CRDs after rebasing lastest develop
1b1802b5 CSPL-1711 (#847)
cb1f5d46 Merge branch 'develop' into feature-biaslang
9c566a52 Merge branch 'CSPL-1425-migration-scenarios' into feature-biaslang
966fbd9c Fixed tests for cmaster Bundle hash
96c13632 CSPL-1794: Create a dummy file to exhaust disk space instead of large files to s3 (#846)
6116dbfa CSPL-1887 - Add telemetry app for Splunk Operator (#842)
1897be0b Addressing comments reviews for kuttl tests
9dafd328 Update BiasLanguageMigration.md
20887e2b Adding Kuttl tests for migration script
62338a7e Delete enterprise.lic
dd3b64bd Delete enterprise.lic
6cfb0f3e Additional temp change needed
5730b60e Adding feature branch for additional testing
d0733797 Merge branch 'feature-biaslang' into CSPL-1425-migration-scenarios
c0db902b Fixed AppFramework Integration tests to recognize new CRDs
59bdd377 Merge branch 'feature-biaslang' into CSPL-1425-migration-scenarios
a8e82a44 Removed temporary code.
e8a79a12 Resolving TestApplyIndexerClusterManager conflicts
2a840e9e Merge branch 'develop' into feature-biaslang
fe377750 Implemented support for Multisite scenarios
fee9d89a Camelcase for node label
e8382b72 Fixing support for Multisite scenarios
706c7521 Update BiasLanguageMigration.md
165bcfa1 Update BiasLanguageMigration.md
77c23540 Merge branch 'develop' into feature-biaslang
4577379c Working version without restarts & Docs Updated
789c43e2 Fixes to work on multiple namespaces
5afa2c53 Implement script to migrate CRs
d954f154 CSPL-1309 - Implement ClusterManager CRD (#795)
f73e1e56 Adjusting LicenseManager post merging AppFramework branch
97084c9c Merge branch 'feature-appframework' into feature-biaslang
b1f627ef Resolving conflicts overwritten by feature-appframework merge
4c130f6d Resolving formating post-changes issues
8fb68e9b Merge branch 'feature-appframework' into feature-biaslang
e267ad52 LicenseManager CRD post-merge adjustments
ab69c258 CSPL-1421 Implement LicenseManager CRD (#756)
