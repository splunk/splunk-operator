# Integration testing for Splunk Operator

## Overview

This README describes the test framework, how to run the various tests and how to write additional tests.

## Test framework

The test framework is based on Ginkgo/Gomega test framework. It is located in the test folder in the splunk-operator repository.
The test framework has 2 main objects: TestEnv and Deployment.

### TestEnv

The TestEnv object represents the test environment that tests are run against. The test environment is a Kubernetes
namespace. When a TestEnv is created, it will create a namespace in the current Kubernetes cluster. The following
resources are then created in that new namespace:

1. Service Account
1. Role
1. RoleBinding
1. Operator/Deployment

The tests are then run within the scope/context of the TestEnv object. Resources used by the tests are scoped to the
TestEnv object (ie Kubernetes namespace). When the tests are completed, the TestEnv object is destroyed and the
resources within are deleted.

### Deployment

The Deployment object is simply to encapsulate what we are deploying within the TestEnv object and help with cleanup.
We can deploy standalone, indexers, search heads, ...etc within a single Deployment object. A TestEnv can have 1 or more
Deployments.

## Running the tests

To run the tests, you will need to create or use an existing Kubernetes cluster. If you have a powerful machine and
Docker engine installed, you can create a local Kubernetes cluster on top of Docker (Kubernetes-IN-Docker aka KIND
cluster) for testing purpose.

To create a KIND cluster, run
> make cluster-up (make cluster-down to tear down the cluster)

If you are using an existing cluster, you will need to install the enterprise CRDs
> kubectl apply -f deploy/crds

To run the test,
> make int-test

This calls the test/run-test.sh script. This test script will recursively runs all the tests in the test folder againsts
the current Kubernetes cluster.

The test will output the results into a junit xml output file.

Note: Both make cluster-up and make int-test invokes the test/deploy-cluster.sh and test/run-test.sh scripts respectively.
Both these scripts take in the env.sh script that defines various environment variables used to build the cluster and run
the test.

- SPLUNK_OPERATOR_IMAGE (Default: splunk/splunk-operator:latest): The splunk operator image to use for testing  
- SPLUNK_ENTERPRISE_IMAGE (Default: splunk/splunk:latest)       : The splunk enterprise image to use for testing
- CLUSTER_PROVIDER (Default: kind)                              : Supports creating eks or kind cluster
- PRIVATE_REGISTRY (Default: localhost:5000)                    : Private registry to use push and pull the images used during testing

Note: To run a specific test, you can

1. cd ./test/{specific-test} folder
2. ginkgo -v -progress --operator-image=localhost:5000/splunk/splunk-operator:latest --splunk-image=localhost:5000/splunk/splunk:latest

### Circleci pipeline

The circleci config.xml file will also run the integration tests when merging to master branch. By default, the pipeline workflow will
deploy a KIND cluster and run the tests against it. To run the test againsts the EKS cluster, you will
need to define the following project environment variables in the circleci console
AWS_ACCESS_KEY_ID
AWS_SECRET_ACCESS_KEY
CLUSTER_PROVIDER=[eks|kind]
ECR_REGISTRY

## Writing tests

Ginkgo test framework has test suite and test spec ("It") files. A test suite file is the main test entry point and represents the test package.
Each test folder is a test package and should have 1 test suite file and 1 or more test spec files.
The test suite file to use to create and delete the TestEnv object. The test spec files are used to create and delete the Deployment object.

### Adding a new test spec

1. To add a new test spec, you can either add a new "It" spec in an existing test spec file or add a new test spec file. If you are adding a new test spec file,
it is best to copy an existing test spec file in that suite and modified it.

### Add a new test suite

1. If you are adding a new test suite (ie you want to run the test in a separate TestEnv or k8s namespace), it is best to copy the test/example folder entirely and modify it

## Notes

1. As mentioned above, by default, we create a TestEnv (aka k8s namespace) for every test suite AND a Deployment for every test spec. It may be time consuming to tear down
the deployment for each test spec especially if we are deploying a large cluster to test. It is possible then to actually create the TestEnv and Deployment at the test suite
level.
