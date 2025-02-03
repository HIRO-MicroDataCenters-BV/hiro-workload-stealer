#!/bin/bash

# Run the initialize script
./scripts/initialize.sh

# Check if the initialize script ran successfully
if [ $? -ne 0 ]; then
    echo "Initialization failed. Exiting."
    exit 1
fi

# Run the install script
./scripts/install.sh

# Check if the install script ran successfully
if [ $? -ne 0 ]; then
    echo "Installation failed. Exiting."
    exit 1
fi

echo "Both scripts ran successfully."