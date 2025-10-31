#!/bin/bash
# Script to stop the Raft cluster

if [ -f logs/cluster.pids ]; then
    echo "Stopping cluster..."
    PIDS=$(cat logs/cluster.pids)
    kill $PIDS 2>/dev/null || true
    echo "Cluster stopped"
    rm logs/cluster.pids
else
    echo "No cluster.pids file found. Cluster may not be running."
fi
