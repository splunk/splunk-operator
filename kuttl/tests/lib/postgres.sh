#!/usr/bin/env bash
# Shared SQL probe. Cluster/client scope is supplied as trailing kubectl flags.
release_image() { yq -er ".artifacts[] | select(.id == \"$1\") | .public.reference" "$2"; }

run_sql() {
    local cluster_name=$1 db=$2 endpoint=$3 statement=$4 client_image=$5 db_name=$6 pod=$7
    local config secret cluster_config ca_ref ca_name ca_key
    shift 7
    config=$(kubectl "$@" get postgresdatabase "$db" -o jsonpath='{.status.databases[0].configMapRef.name}')
    secret=$(kubectl "$@" get postgresdatabase "$db" -o jsonpath='{.status.databases[0].adminUserSecretRef.name}')
    cluster_config=$(kubectl "$@" get postgrescluster "$cluster_name" -o jsonpath='{.status.resources.configMapRef.name}')
    ca_ref=$(kubectl "$@" get configmap "$cluster_config" -o jsonpath='{.data.SERVER_CA_SECRET_REF}')
    ca_name=${ca_ref%/*}; ca_key=${ca_ref#*/}
    kubectl "$@" delete "pod/$pod" --ignore-not-found --wait --timeout=60s
    jq -n --arg image "$client_image" --arg pod "$pod" --arg database "$db_name" --arg config "$config" --arg secret "$secret" \
        --arg endpoint "$endpoint" --arg ca "$ca_name" --arg key "$ca_key" --arg sql "$statement" \
        '{apiVersion:"v1",kind:"Pod",metadata:{name:$pod},spec:{restartPolicy:"Never",
          containers:[{name:"psql",image:$image,command:["/bin/sh","-ec","psql -v ON_ERROR_STOP=1 -c \"$CHECK_SQL\""],
            env:[{name:"CHECK_SQL",value:$sql},{name:"PGDATABASE",value:$database},
              {name:"PGHOST",valueFrom:{configMapKeyRef:{name:$config,key:$endpoint}}},
              {name:"PGPORT",value:"5432"},{name:"PGSSLMODE",value:"verify-full"},
              {name:"PGSSLROOTCERT",value:"/ca/ca.crt"},
              {name:"PGUSER",valueFrom:{secretKeyRef:{name:$secret,key:"username"}}},
              {name:"PGPASSWORD",valueFrom:{secretKeyRef:{name:$secret,key:"password"}}}],
            volumeMounts:[{name:"ca",mountPath:"/ca",readOnly:true}]}],
          volumes:[{name:"ca",secret:{secretName:$ca,items:[{key:$key,path:"ca.crt"}]}}]}}' | kubectl "$@" apply -f -
    if ! kubectl "$@" wait --for=jsonpath='{.status.phase}'=Succeeded "pod/$pod" --timeout=180s; then
        kubectl "$@" logs "$pod"; return 1
    fi
}
