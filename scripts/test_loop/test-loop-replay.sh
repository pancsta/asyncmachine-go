#!/bin/bash

#set -x
LOCATION=~/.local/share/rr/rpc.test-2/

dlv replay $LOCATION --headless --listen=:2345 --log --api-version=2
