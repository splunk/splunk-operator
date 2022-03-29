This is an upgrade test case for Clustered deployment (C3 - clustered indexer, search head cluster)
1. Install splunk operator 1.0.5 to specific namespace
2. Make sure the installation is complete and all the CRD are installed along with deployment
3. Create C3 deployment  custom resources - cluter master, indexer cluster, search head cluster
4. Check C3 deployment instanances are up and running
5. Upgrade splunk operator to 1.1.0 using upgrade script - step 04
6. Check the old operator is cleaned up and latest operator is installed in splunk-operator namespace
7. Check C3 deployment instanances are up and running
8. Cleanup C3 instanances
9. Cleanup splunk operator 1.1.0