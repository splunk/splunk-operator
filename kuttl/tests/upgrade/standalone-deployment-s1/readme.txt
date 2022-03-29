This is an upgrade test case for Clustered deployment (s1 - standalone)
1. Install splunk operator 1.0.5 to specific namespace
2. Make sure the installation is complete and all the CRD are installed along with deployment
3. Create S1 deployment  custom resources - standalone custom resource
4. Check S1 deployment instanances are up and running
5. Upgrade splunk operator to 1.1.0 using upgrade script - step 04
6. Check the old opeartor is cleaned up and latest operator is installed in splunk-operator namespace
7. Check S1 deployment instanances are up and running
8. Cleanup standalone s1 instanances
9. Cleanup splunk operator 1.1.0