#!/bin/bash

STUNNER_PUBLIC_ADDR="null"
TIMERCNT=0

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
((TIMERCNT++))

#this is triggered if the external IP address didn't occure for 120 seconds
if [[ "$TIMERCNT" -eq 120 ]]; then
  STUNNER_PUBLIC_ADDR=$(curl -s --cacert ${CACERT} --header "Authorization: Bearer ${TOKEN}" -X GET ${APISERVER}/api/v1/nodes / |
    jq '.items[0].status.addresses[] | select(.type=="ExternalIP").address')
  STUNNER_PUBLIC_PORT=$(curl -s --cacert ${CACERT} --header "Authorization: Bearer ${TOKEN}" -X GET ${APISERVER}/api/v1/namespaces/${NAMESPACE}/services/stunner / |
    jq  '.spec.ports[0].nodePort')
  JSON_STRING=$( jq -n \
                  --argjson addr $STUNNER_PUBLIC_ADDR \
                  --argjson port $STUNNER_PUBLIC_PORT \
                  '[{"op": "add", "path": "/data/STUNNER_PUBLIC_ADDR", "value": $addr},{"op": "add", "path": "/data/STUNNER_PUBLIC_PORT", "value": $port|tostring}]' )
  
  echo "No external IP has been found for the Stunner LoadBalancer Service"
  echo "Falling back to use NodePort"
  echo $JSON_STRING

  curl -k \
  -X PATCH \
  -d "${JSON_STRING}" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Accept: application/json' \
  -H 'Content-Type: application/json-patch+json' \
  ${APISERVER}/api/v1/namespaces/${NAMESPACE}/configmaps/stunner-config

  exit $?
fi

done

STUNNER_PUBLIC_PORT=$(curl -s --cacert ${CACERT} --header "Authorization: Bearer ${TOKEN}" -X GET ${APISERVER}/api/v1/namespaces/${NAMESPACE}/services/stunner / |
  jq  '.spec.ports[0].port')

JSON_STRING=$( jq -n \
                  --argjson addr $STUNNER_PUBLIC_ADDR \
                  --argjson port $STUNNER_PUBLIC_PORT \
                  '[{"op": "add", "path": "/data/STUNNER_PUBLIC_ADDR", "value": $addr},{"op": "add", "path": "/data/STUNNER_PUBLIC_PORT", "value": $port|tostring}]' )
echo 'External IP has been found for the Stunner LoadBalancer Service ' 
echo $JSON_STRING

curl -k \
  -X PATCH \
  -d "${JSON_STRING}" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Accept: application/json' \
  -H 'Content-Type: application/json-patch+json' \
  ${APISERVER}/api/v1/namespaces/${NAMESPACE}/configmaps/stunner-config
