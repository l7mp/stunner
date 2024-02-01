#!/bin/bash

USAGE="stunnerctl running-config <namespace/name>"

[ -z "$1" -o -z "$2" ] && echo $USAGE && exit 0
COMMAND="$1"
ARG="$2"

# stop the port-forwarder
trap "trap - SIGTERM && kill -- -$$" SIGINT SIGTERM EXIT

jq=$(which jq)
if [ -z "$jq" ] ; then
    echo "Error: cannot find jq in PATH" && exit 0
fi
jq="$jq -r"

# dumps only the first listener
running_config () {
    args=(${ARG//\// })
    namespace=${args[0]}
    name=${args[1]}
    [ -z $namespace -o -z $name ] && echo "cannot parse <namespace/name> argument" && exit 0

    # find the CDS server
    CDS_SERVER_NAME=$(kubectl get pods -l stunner.l7mp.io/config-discovery-service=enabled --all-namespaces -o jsonpath='{.items[0].metadata.name}')
    CDS_SERVER_NAMESPACE=$(kubectl get pods -l stunner.l7mp.io/config-discovery-service=enabled --all-namespaces -o jsonpath='{.items[0].metadata.namespace}')
    [ -z $CDS_SERVER_NAME -o -z $CDS_SERVER_NAMESPACE ] && echo "Could not find CDS server" && exit 1

    # start the port-forwarder
    kubectl -n $CDS_SERVER_NAMESPACE port-forward pod/${CDS_SERVER_NAME}  63478:13478 >/dev/null 2>&1 &

    # query the cds server
    sleep 1
    tmpfile=$(mktemp "./stunnerd-config.XXXXXX")
    curl -s http://127.0.0.1:63478/api/v1/configs/${namespace}/${name} > $tmpfile

    if grep -q "onfig not found" $tmpfile >/dev/null 2>&1; then
       cat $tmpfile
       exit 1
    fi
    
    local AUTH_TYPE=$($jq ".auth.type" $tmpfile)
    [ $AUTH_TYPE == "plaintext" ] && AUTH_TYPE="static"
    [ $AUTH_TYPE == "longterm" ]  && AUTH_TYPE="ephemeral"
    
    local USERNAME=$($jq ".auth.credentials.username" $tmpfile)
    local PASSWORD=$($jq ".auth.credentials.password" $tmpfile)
    local SECRET=$($jq ".auth.credentials.secret" $tmpfile)

    [ -n "$AUTH_TYPE" -a "$AUTH_TYPE" != "null" ] && echo -e "STUN/TURN authentication type:\t$AUTH_TYPE"
    [ -n "$USERNAME" -a "$USERNAME" != "null" ]   && echo -e "STUN/TURN username:\t\t$USERNAME"
    [ -n "$PASSWORD" -a "$PASSWORD" != "null" ]   && echo -e "STUN/TURN password:\t\t$PASSWORD"
    [ -n "$SECRET" -a "$SECRET" != "null" ]       && echo -e "STUN/TURN secret:\t\t$SECRET"

    if [ $($jq '.listeners | length' $tmpfile) -gt 0 ]; then {
        local n=0

        for NAME in $($jq '.listeners[] | .name' $tmpfile); do
            local PROTOCOL=$($jq ".listeners[${n}].protocol" $tmpfile)
            local PUBLIC_ADDR=$($jq ".listeners[${n}].public_address" $tmpfile)
            local PUBLIC_PORT=$($jq ".listeners[${n}].public_port" $tmpfile)

            n=$((n+1))

            echo -e "Listener ${n}"
            [ -n "$NAME" -a "$NAME" != "null" ]               && echo -e "\tName:\t$NAME"
            [ -n "$NAME" -a "$NAME" != "null" ]               && echo -e "\tListener:\t$NAME"
            [ -n "$PROTOCOL" -a "$PROTOCOL" != "null" ]       && echo -e "\tProtocol:\t$PROTOCOL"
            [ -n "$PUBLIC_ADDR" -a "$PUBLIC_ADDR" != "null" ] && echo -e "\tPublic address:\t$PUBLIC_ADDR"
            [ -n "$PUBLIC_PORT" -a "$PUBLIC_PORT" != "null" ] && echo -e "\tPublic port:\t$PUBLIC_PORT"
        done
    } else {
        echo No listeners
    }
    fi

    rm -f $tmpfile
}

case $COMMAND in
    "running-config")
        running_config
        ;;

    *)
        echo "Unknown command: '$COMMAND'"
        exit 0
        ;;
esac

exit 1
