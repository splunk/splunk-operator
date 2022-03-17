Test Steps
    ################## SETUP ####################
    * Upload V1 apps to S3 for Monitoring Console
    * Create app source for Monitoring Console
    * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
    * Upload V1 apps to S3 for Indexer Cluster and Search Head Cluster
    * Create app sources for Cluster Manager and Deployer
    * Prepare and deploy C3 CRD with app framework and wait for the pods to be ready
    ######### INITIAL VERIFICATIONS #############
    * Verify V1 apps are downloaded on Cluster Manager, Deployer and Monitoring Console
    * Verify bundle push is successful
    * Verify V1 apps are copied, installed on Monitoring Console and on Search Heads and Indexers pods
    ############### UPGRADE APPS ################
    * Upload V2 apps on S3
    * Wait for Monitoring Console and C3 pods to be ready
    ############ FINAL VERIFICATIONS ############
    * Verify V2 apps are downloaded on Cluster Manager, Deployer and Monitoring Console
    * Verify bundle push is successful
    * Verify V2 apps are copied and upgraded on Monitoring Console and on Search Heads and Indexers pods