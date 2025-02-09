#!/bin/bash

CLUSTER_NAME=${1:-local}
echo "Create Kind cluster"

echo "Delete and Create a 'kind' cluster with name '$CLUSTER_NAME'"
kind delete cluster --name $CLUSTER_NAME
kind create cluster --name $CLUSTER_NAME

echo "Set the kubectl context to $CLUSTER_NAME cluster"
kubectl cluster-info --context kind-$CLUSTER_NAME
