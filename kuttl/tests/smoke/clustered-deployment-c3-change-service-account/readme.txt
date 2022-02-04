Clustered deployment (C3 - clustered indexer, search head cluster
------------------------------------------------------------------
Scenario:
--------
0. create service account 
1. create clustermanager custom resource add step 0 service account to spec
2. create indexer cluster custom resource add step 0 service account to spec
3. create search head cluster custom resource add step 0 service account to spec
4. create new service account
5. update clustermanager custom resource add step 4 service account to spec
6. update indexer cluster custom resource add step 4 service account to spec
7. update search head cluster custom resource add step 4 service account to spec
8. remove finalizer from cluster manager 
9. remove finalizer from indexer cluster
10. remove finalizer from search head cluster
11 remove search head master
12. remove indexer cluster
13. remove cluster master