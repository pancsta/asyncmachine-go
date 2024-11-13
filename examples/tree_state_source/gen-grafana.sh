#!/usr/bin/env bash

# root
go ../../tools/cmd/am-gen grafana
  --name root
  --folder tree_state_source
  --ids root,_rm-root,_rs-root-0,_rs-root-1,_rs-root-2
  --grafana-url http://localhost:3000
  --source tree_state_source_root

# replicant 1
go ../../tools/cmd/am-gen grafana
  --name rep-1
  --folder tree_state_source
  --grafana-url http://localhost:3000
  --source tree_state_source_rep_1

# replicant 2
go ../../tools/cmd/am-gen grafana
  --name rep-2
  --folder tree_state_source
  --grafana-url http://localhost:3000
  --source tree_state_source_rep_2

# replicant 1 of replicant 1
go ../../tools/cmd/am-gen grafana
  --name rep-1-1
  --folder tree_state_source
  --grafana-url http://localhost:3000
  --source tree_state_source_rep_1_1

# replicant 2 of replicant 1
go ../../tools/cmd/am-gen grafana
  --name rep-1-2
  --folder tree_state_source
  --grafana-url http://localhost:3000
  --source tree_state_source_rep_1_2

# replicant 1 of replicant 2
go ../../tools/cmd/am-gen grafana
  --name rep-2-1
  --folder tree_state_source
  --grafana-url http://localhost:3000
  --source tree_state_source_rep_2_1

# replicant 2 of replicant 2
go ../../tools/cmd/am-gen grafana
  --name rep-2-2
  --folder tree_state_source
  --grafana-url http://localhost:3000
  --source tree_state_source_rep_2_2
