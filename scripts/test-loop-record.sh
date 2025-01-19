#!/bin/bash

set -x
PKG=pkg/rpc
TEST=TestRetryConn

while true; do
  echo "Running tests..."
  task clean

  # compile
  go test ./$PKG -gcflags 'all=-N -l' -c
#  go test ./$PKG -c

  # run & record
  env AM_TEST_RUNNER=1 \
    rr record rpc.test -- -test.failfast -test.parallel 1 -test.v -test.run ^${TEST}\$
  #  rr record rpc.test -- -test.failfast -test.parallel 1 -test.v
  #  rr record rpc.test

  # stop
  if [ $? -ne 0 ]; then
    echo "encountered non-zero exit code: $?";
    exit;
  fi

done