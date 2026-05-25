#!/bin/bash

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

NAMESPACE="${NAMESPACE:-default}"
POSTGRES_CLUSTER_NAME="${1:-}"

if [ -z "$POSTGRES_CLUSTER_NAME" ]; then
    echo -e "${RED}Error: PostgresCluster name is required${NC}"
    echo "Usage: $0 <postgres-cluster-name> [namespace]"
    echo "Example: $0 my-postgres-cluster default"
    exit 1
fi

if [ -n "$2" ]; then
    NAMESPACE="$2"
fi

echo -e "${YELLOW}Connecting to PostgresCluster: $POSTGRES_CLUSTER_NAME in namespace: $NAMESPACE${NC}"

# Fetch PostgresCluster status once
CLUSTER_JSON=$(kubectl get postgrescluster "$POSTGRES_CLUSTER_NAME" -n "$NAMESPACE" -o json 2>/dev/null)

CONFIGMAP_NAME=$(echo "$CLUSTER_JSON" | jq -r '.status.resources.configMapRef.name // empty')
SECRET_NAME=$(echo "$CLUSTER_JSON" | jq -r '.status.resources.superUserSecretRef.name // empty')

if [ -z "$CONFIGMAP_NAME" ]; then
    echo -e "${RED}Error: ConfigMap reference not found in PostgresCluster status${NC}"
    echo "Make sure the PostgresCluster is ready and the ConfigMap has been created"
    exit 1
fi

if [ -z "$SECRET_NAME" ]; then
    echo -e "${RED}Error: Secret reference not found in PostgresCluster status${NC}"
    echo "Make sure the PostgresCluster is ready and the Secret has been created"
    exit 1
fi

echo -e "${GREEN}Found ConfigMap: $CONFIGMAP_NAME${NC}"
echo -e "${GREEN}Found Secret: $SECRET_NAME${NC}"

# Fetch ConfigMap once, extract all fields
echo -e "\n${YELLOW}Extracting connection details...${NC}"
CM_JSON=$(kubectl get configmap "$CONFIGMAP_NAME" -n "$NAMESPACE" -o json)
DB_PORT=$(echo "$CM_JSON" | jq -r '.data.DEFAULT_CLUSTER_PORT')
DB_USER=$(echo "$CM_JSON" | jq -r '.data.SUPER_USER_NAME')
RW_SERVICE_FQDN=$(echo "$CM_JSON" | jq -r '.data.CLUSTER_RW_ENDPOINT')
RO_SERVICE_FQDN=$(echo "$CM_JSON" | jq -r '.data.CLUSTER_RO_ENDPOINT')
R_SERVICE_FQDN=$(echo "$CM_JSON" | jq -r '.data.CLUSTER_R_ENDPOINT')


# Extract password from Secret
DB_PASSWORD=$(kubectl get secret "$SECRET_NAME" -n "$NAMESPACE" -o jsonpath='{.data.password}' | base64 -d)

# Database name from CNPG cluster bootstrap config
DB_NAME=$(kubectl get cluster "$POSTGRES_CLUSTER_NAME" -n "$NAMESPACE" \
    -o jsonpath='{.spec.bootstrap.initdb.database}' 2>/dev/null)
DB_NAME="${DB_NAME:-postgres}"

echo -e "${GREEN}Connection Details:${NC}"
echo "  RW Service: $RW_SERVICE_FQDN"
echo "  RO Service: $RO_SERVICE_FQDN"
echo "  R Service: $R_SERVICE_FQDN"
echo "  Port: $DB_PORT"
echo "  Database: $DB_NAME"
echo "  User: $DB_USER"

echo -e "\n${YELLOW}Spawning postgres client pod in namespace $NAMESPACE...${NC}"
kubectl run postgres-client-test \
    --rm -i --tty \
    --image=postgres:16 \
    --restart=Never \
    --namespace="$NAMESPACE" \
    --env="PGPASSWORD=$DB_PASSWORD" \
    -- psql "postgresql://$DB_USER@$RW_SERVICE_FQDN:$DB_PORT/$DB_NAME?sslmode=require"
