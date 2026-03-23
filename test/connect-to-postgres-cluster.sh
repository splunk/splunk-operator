#!/bin/bash
# filepath: scripts/test-postgres-connection.sh

set -e

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Default values
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

# Get ConfigMap name from PostgresCluster status
CONFIGMAP_NAME=$(kubectl get postgrescluster "$POSTGRES_CLUSTER_NAME" -n "$NAMESPACE" \
    -o jsonpath='{.status.resources.configMapRef.name}' 2>/dev/null)

if [ -z "$CONFIGMAP_NAME" ]; then
    echo -e "${RED}Error: ConfigMap reference not found in PostgresCluster status${NC}"
    echo "Make sure the PostgresCluster is ready and the ConfigMap has been created"
    exit 1
fi

# Get Secret name from PostgresCluster status
SECRET_NAME=$(kubectl get postgrescluster "$POSTGRES_CLUSTER_NAME" -n "$NAMESPACE" \
    -o jsonpath='{.status.resources.secretRef.name}' 2>/dev/null)

if [ -z "$SECRET_NAME" ]; then
    echo -e "${RED}Error: Secret reference not found in PostgresCluster status${NC}"
    echo "Make sure the PostgresCluster is ready and the Secret has been created"
    exit 1
fi

echo -e "${GREEN}Found ConfigMap: $CONFIGMAP_NAME${NC}"
echo -e "${GREEN}Found Secret: $SECRET_NAME${NC}"

# Extract connection details from ConfigMap (using correct uppercase keys)
echo -e "\n${YELLOW}Extracting connection details...${NC}"
DB_PORT=$(kubectl get configmap "$CONFIGMAP_NAME" -n "$NAMESPACE" -o jsonpath='{.data.DEFAULT_CLUSTER_PORT}')
DB_USER=$(kubectl get configmap "$CONFIGMAP_NAME" -n "$NAMESPACE" -o jsonpath='{.data.SUPER_USER_NAME}')
RW_SERVICE_FQDN=$(kubectl get configmap "$CONFIGMAP_NAME" -n "$NAMESPACE" -o jsonpath='{.data.CLUSTER_RW_ENDPOINT}')
RO_SERVICE_FQDN=$(kubectl get configmap "$CONFIGMAP_NAME" -n "$NAMESPACE" -o jsonpath='{.data.CLUSTER_RO_ENDPOINT}')
R_SERVICE_FQDN=$(kubectl get configmap "$CONFIGMAP_NAME" -n "$NAMESPACE" -o jsonpath='{.data.CLUSTER_R_ENDPOINT}')

# Extract just the service name (first part before the dot)
RW_SERVICE=$(echo "$RW_SERVICE_FQDN" | cut -d'.' -f1)
RO_SERVICE=$(echo "$RO_SERVICE_FQDN" | cut -d'.' -f1)
R_SERVICE=$(echo "$R_SERVICE_FQDN" | cut -d'.' -f1)

# Extract password from Secret
DB_PASSWORD=$(kubectl get secret "$SECRET_NAME" -n "$NAMESPACE" -o jsonpath='{.data.password}' | base64 -d)

# Get database name from CNPG cluster (assuming it matches the PostgresCluster name or is 'app')
DB_NAME=$(kubectl get cluster "$POSTGRES_CLUSTER_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.bootstrap.initdb.database}' 2>/dev/null || echo "postgres")

echo -e "${GREEN}Connection Details:${NC}"
echo "  RW Service: $RW_SERVICE_FQDN"
echo "  RO Service: $RO_SERVICE_FQDN"
echo "  R Service: $R_SERVICE_FQDN"
echo "  Port: $DB_PORT"
echo "  Database: $DB_NAME"
echo "  User: $DB_USER"

# Check if psql is installed
if ! command -v psql &> /dev/null; then
    echo -e "\n${YELLOW}psql client not found. Using kubectl run with postgres image...${NC}"
    
    echo -e "${YELLOW}Creating temporary pod for connection test...${NC}"
    
    kubectl run postgres-client-test \
        --rm -i --tty \
        --image=postgres:16 \
        --restart=Never \
        --namespace="$NAMESPACE" \
        --env="PGPASSWORD=$DB_PASSWORD" \
        -- psql -h "$RW_SERVICE_FQDN" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME"
else
    # Use port-forward for local connection
    echo -e "\n${YELLOW}Setting up port-forward to PostgreSQL service...${NC}"
    
    # Kill any existing port-forward on 5432
    pkill -f "kubectl.*port-forward.*$RW_SERVICE" 2>/dev/null || true
    
    # Start port-forward in background (use service name only, not FQDN)
    kubectl port-forward -n "$NAMESPACE" "service/$RW_SERVICE" 5432:$DB_PORT > /dev/null 2>&1 &
    PORT_FORWARD_PID=$!
    
    # Cleanup function
    cleanup() {
        echo -e "\n${YELLOW}Cleaning up port-forward...${NC}"
        kill $PORT_FORWARD_PID 2>/dev/null || true
    }
    trap cleanup EXIT
    
    # Wait for port-forward to be ready
    echo -e "${YELLOW}Waiting for port-forward to be ready...${NC}"
    sleep 3
    
    echo -e "${GREEN}Connecting to PostgreSQL...${NC}"
    echo -e "${YELLOW}Password: $DB_PASSWORD${NC}\n"
    
    # Use connection string format which is more reliable
    # Disable GSSAPI and use password authentication only
    PGPASSWORD="$DB_PASSWORD" psql "postgresql://$DB_USER@localhost:5432/$DB_NAME?gssencmode=disable" \
        || PGPASSWORD="$DB_PASSWORD" psql -h localhost -p 5432 -U "$DB_USER" -d "$DB_NAME" --no-psqlrc
fi