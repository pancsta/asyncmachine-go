#!/usr/bin/env sh

# check if installed
if command -v task >/dev/null; then echo "OK" && exit; fi

echo "Visit https://taskfile.dev/installation/ for more info"
echo "----- ----- -----"

sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d -b ~/.local/bin
