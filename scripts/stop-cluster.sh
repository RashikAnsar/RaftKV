#!/bin/bash
# Script to stop the Raft cluster

set -e

echo "Stopping Raft cluster..."

# Try PID file first
if [ -f logs/cluster.pids ]; then
    echo "Using PID file..."
    PIDS=$(cat logs/cluster.pids)
    for PID in $PIDS; do
        if kill -0 $PID 2>/dev/null; then
            echo "  Stopping process $PID..."
            kill $PID 2>/dev/null || true
        fi
    done
    rm logs/cluster.pids
    echo "Cluster stopped (from PID file)"
else
    echo "No cluster.pids file found. Searching for processes..."
    # Fallback: kill by pattern
    PIDS=$(pgrep -f "kvstore.*raft" || true)
    if [ -n "$PIDS" ]; then
        echo "Found processes: $PIDS"
        for PID in $PIDS; do
            echo "  Stopping process $PID..."
            kill $PID 2>/dev/null || true
        done
        echo "Cluster stopped (by process search)"
    else
        echo "No kvstore processes found"
    fi
fi

# Wait for processes to terminate
sleep 1

# Verify all processes are stopped
REMAINING=$(pgrep -f "kvstore.*raft" || true)
if [ -n "$REMAINING" ]; then
    echo "Some processes still running: $REMAINING"
    echo "Force killing..."
    kill -9 $REMAINING 2>/dev/null || true
fi

echo ""
echo "Cluster status:"
if pgrep -f "kvstore.*raft" > /dev/null; then
    echo "Some processes still running"
else
    echo "All processes stopped"
fi
