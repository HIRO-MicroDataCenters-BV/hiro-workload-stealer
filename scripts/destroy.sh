#!/bin/bash
CLUSTER_NAME=${1:-local}
echo "Delete and Create a 'kind' cluster with name '$CLUSTER_NAME'"
kind delete cluster --name $CLUSTER_NAME