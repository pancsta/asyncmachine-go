#!/usr/bin/env sh

echo "----- WARMUP"
echo "----- WARMUP"
echo "----- WARMUP"
go-wrk -c 512 -d 1 "http://caddy"
sleep 3

echo "----- BENCHMARK"
echo "----- BENCHMARK"
echo "----- BENCHMARK"
echo $(date)
go-wrk -c 512 -d 10 "http://caddy"
