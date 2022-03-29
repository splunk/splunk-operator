This is an upgrade test case for Clustered deployment (C3 - clustered indexer, search head cluster
1. install splunk operator 1.0.5 to specific namespace
2. make sure the installation is complete and all the CRD are installed along with deployment
3. create C3 deployment  custom resources - cluter master, indexer cluster, search head cluster
4. check C3 deployment instanances are up and running
5. upgrade splunk operator to 1.1.0 using upgrade script - step 04
6. check the old opeartor is cleaned up and latest operator is installed in splunk-operator namespace
7. check C3 deployment instanances are up and running
8. Cleanup C3 instanances
9. Cleanup splunk operator 1.1.0