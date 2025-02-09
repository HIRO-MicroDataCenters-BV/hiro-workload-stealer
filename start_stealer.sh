#!/bin/bash

CLUSTER_NAME=${1:-local}
# Run the initialize script
./scripts/initialize.sh $CLUSTER_NAME

# Check if the initialize script ran successfully
if [ $? -ne 0 ]; then
    echo "Initialization failed. Exiting."
    exit 1
fi

# Run the install script
./scripts/install.sh $CLUSTER_NAME

# Check if the install script ran successfully
if [ $? -ne 0 ]; then
    echo "Installation failed. Exiting."
    exit 1
fi

echo "Both scripts ran successfully."