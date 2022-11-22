#! /bin/bash

uninstall=false
issuer="ingress/issuer.yaml"

while getopts u:i: flag
do
    case "${flag}" in
        u) uninstall=${OPTARG};;
        i) issuer=${OPTARG};;
    esac
done

if [ "$uninstall" = false ] ; then
    # Install nginx-ingress
    helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
    helm repo update
    helm install nginx ingress-nginx/ingress-nginx
    echo "Wait 60 seconds"
    sleep 60
    kubectl get all

    # Install cert-manager
    helm repo add jetstack https://charts.jetstack.io
    helm repo update
    kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.9.1/cert-manager.crds.yaml
    helm install cert-manager jetstack/cert-manager --namespace cert-manager --create-namespace --version v1.9.1 --set global.leaderElection.namespace=cert-manager --timeout 600s --debug
    echo "Wait 60 seconds"
    sleep 60
    kubectl -n cert-manager get all

    # Install cluster issuer
    kubectl apply -f "${issuer}"
    echo "Wait 10 seconds"
    sleep 10
else
    # Delete issuer
    kubectl delete -f "${issuer}"

    # Delete cert-manager
    kubectl delete -f https://github.com/cert-manager/cert-manager/releases/download/v1.9.1/cert-manager.crds.yaml
    helm delete cert-manager -n cert-manager

    # Delete nginx-ingress
    helm delete nginx
fi

