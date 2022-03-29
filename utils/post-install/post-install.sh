#!/bin/bash

STUNNER_PUBLIC_ADDR="null"

# Point to the internal API server hostname
APISERVER=https://kubernetes.default.svc

# Path to ServiceAccount token
SERVICEACCOUNT=/var/run/secrets/kubernetes.io/serviceaccount

# Read this Pod's namespace
NAMESPACE=$(cat ${SERVICEACCOUNT}/namespace)

# Read the ServiceAccount bearer token
TOKEN=$(cat ${SERVICEACCOUNT}/token)

# Reference the internal certificate authority (CA)
CACERT=${SERVICEACCOUNT}/ca.crt

while [[ "$STUNNER_PUBLIC_ADDR" == "null" ]]
do
STUNNER_PUBLIC_ADDR=$(curl -s --cacert ${CACERT} --header "Authorization: Bearer ${TOKEN}" -X GET ${APISERVER}/api/v1/namespaces/${NAMESPACE}/services/stunner / | 
jq  '.status.loadBalancer.ingress[0].ip')
sleep 1
done

JSON_STRING=$( jq -n \
                  --argjson addr $STUNNER_PUBLIC_ADDR \
                  '[{"op": "add", "path": "/data/STUNNER_PUBLIC_ADDR", "value": $addr}]' )

curl -k \
  -X PATCH \
  -d "${JSON_STRING}" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Accept: application/json' \
  -H 'Content-Type: application/json-patch+json' \
  ${APISERVER}/api/v1/namespaces/${NAMESPACE}/configmaps/stunner-config
