# CDC and Cross-DC Replication

> Change Data Capture (CDC) pipeline and cross-datacenter replication for RaftKV. Covers the event model, publisher-subscriber bus, gRPC streaming, remote applier, vector clocks, topology management, and lag monitoring.

## Table of Contents

- [CDC and Cross-DC Replication](#cdc-and-cross-dc-replication)
  - [Table of Contents](#table-of-contents)
  - [Overview](#overview)
  - [Current Implementation Status](#current-implementation-status)
  - [CDC Subsystem](#cdc-subsystem)
    - [ChangeEvent Model](#changeevent-model)
    - [Publisher](#publisher)
    - [Subscriber](#subscriber)
    - [Event Filters](#event-filters)
  - [Cross-DC Replication](#cross-dc-replication)
    - [StreamServer (gRPC)](#streamserver-grpc)
    - [ChangeApplier](#changeapplier)
    - [Conflict Resolution](#conflict-resolution)
  - [Vector Clocks](#vector-clocks)
    - [Clock Operations](#clock-operations)
    - [Causality Relationships](#causality-relationships)
    - [Vector Clock Update Sequence](#vector-clock-update-sequence)
  - [Topology Management](#topology-management)
    - [Replication Topology Graph](#replication-topology-graph)
  - [Lag Monitoring](#lag-monitoring)
    - [Lag Monitoring Flow](#lag-monitoring-flow)
  - [End-to-End CDC Event Flow](#end-to-end-cdc-event-flow)
  - [gRPC API](#grpc-api)
  - [Operational Considerations](#operational-considerations)
  - [See Also](#see-also)

---

## Overview

RaftKV's CDC system captures every committed write from the Raft FSM and publishes it as a `ChangeEvent`. These events flow through an in-process pub-sub bus and are streamed over gRPC to remote datacenters, where they are applied by a `ChangeApplier`.

The design targets **Active-Passive** replication: one primary datacenter accepts writes, secondary datacenters receive changes and serve reads. Active-Active (bidirectional) replication is architecturally prepared (vector clocks are present) but the conflict resolution for concurrent writes from multiple DCs is currently Last-Write-Wins (LWW) with a datacenter-ID tiebreaker.

---

## Current Implementation Status

| Feature | Status | Notes |
|---|---|---|
| CDC event capture from FSM | Implemented | Called from `FSM.Apply()` |
| Publisher / Subscriber bus | Implemented | Buffered channels, non-blocking publish |
| gRPC streaming server | Implemented | `ReplicationService.StreamChanges` |
| Remote change applier | Implemented | Idempotent, LWW conflict resolution |
| Vector clock tracking | Implemented | `Increment`, `Merge`, `Compare` |
| Vector clock attached to events | Partial | Field present in proto and `ChangeEvent`, not yet populated by FSM |
| Active-Active conflict resolution | Scaffolded | LWW implemented; vector-clock-based resolution planned |
| Topology health checking | Implemented | Periodic gRPC heartbeat |
| Topology TLS | Not implemented | `connectDatacenter` uses `insecure.NewCredentials()` |
| Lag monitoring | Implemented | Per-DC lag in entries and seconds |

---

## CDC Subsystem

### ChangeEvent Model

`ChangeEvent` (`internal/cdc/event.go`) is the unit of change captured from the Raft FSM.

```go
type ChangeEvent struct {
    RaftIndex    uint64            // Raft log index of the committed entry
    RaftTerm     uint64            // Raft term
    Operation    string            // "put" or "delete"
    Key          string
    Value        []byte
    Timestamp    time.Time
    DatacenterID string            // Source DC — set by Publisher on Publish()
    SequenceNum  uint64            // Monotonically increasing CDC sequence number
    VectorClock  map[string]uint64 // Per-DC logical clock (populated in future phase)
}
```

`ChangeEvent` is **cloned** before delivery to each subscriber to prevent data races across goroutines. `Clone` performs a deep copy of `Value` and `VectorClock`.

### Publisher

`Publisher` (`internal/cdc/publisher.go`) is the fan-out hub. It holds a map of registered `Subscriber`s and broadcasts each event to all of them.

```mermaid
graph LR
    FSM["Raft FSM\n(Apply)"]
    PUB["Publisher\n.Publish(event)"]
    S1["Subscriber A\n(stream-dc1)"]
    S2["Subscriber B\n(stream-dc2)"]
    S3["Subscriber C\n(custom)"]

    FSM --> PUB
    PUB -->|"clone + non-blocking send"| S1
    PUB -->|"clone + non-blocking send"| S2
    PUB -->|"clone + non-blocking send"| S3
```

**Key behaviors:**
- `Publish` is called with a read lock so concurrent publishes to different subscribers happen concurrently.
- Each subscriber channel send is **non-blocking** (`select { case ch <- event: default: drop }`). If a subscriber's buffer is full, the event is dropped and `metricsDropped` is incremented. This prevents a slow consumer from blocking the FSM.
- The `DatacenterID` and monotonically increasing `SequenceNum` are stamped on the event by the publisher before fan-out.

### Subscriber

`Subscriber` (`internal/cdc/subscriber.go`) wraps a buffered channel.

| Method | Description |
|---|---|
| `Events()` | Returns the read-only event channel (for `range` loops) |
| `Recv(ctx)` | Blocks until event available or context cancelled |
| `RecvWithTimeout(d)` | Recv with deadline |
| `TryRecv()` | Non-blocking; returns nil if queue empty |
| `Close()` | Closes the channel; subsequent `Recv` returns `ErrSubscriberClosed` |
| `Stats()` | Returns queue length, capacity, dropped count |

Default buffer size for the stream subscriber: **1000 events**.

### Event Filters

Filters are predicate functions `EventFilter func(*ChangeEvent) bool` applied by the publisher before delivering to a subscriber.

| Filter | Description |
|---|---|
| `KeyPrefixFilter(prefix)` | Matches events where `Key` starts with `prefix` |
| `OperationFilter(op)` | Matches events by operation (`"put"` or `"delete"`) |
| `CompositeFilter(f1, f2, ...)` | AND of multiple filters |

---

## Cross-DC Replication

### StreamServer (gRPC)

`StreamServer` (`internal/replication/stream.go`) implements `ReplicationService.StreamChanges`. It accepts incoming gRPC streaming connections from remote DCs and delivers CDC events in real time.

For each incoming stream:
1. A new `Subscriber` is registered with the local `Publisher` (filtered by optional `KeyPrefix`).
2. Events are read from the subscriber and filtered by `RaftIndex > req.FromIndex` (to support resumption after reconnect).
3. Each event is converted to the protobuf `ChangeEvent` message and sent over the gRPC server stream.
4. On disconnect, the subscriber is deregistered.

The `StreamServer` also exposes:
- `GetReplicationStatus` — returns lag info per connected DC.
- `Heartbeat` — health-check endpoint used by remote topology managers.

### ChangeApplier

`ChangeApplier` (`internal/replication/applier.go`) runs on the **receiving** (secondary) DC and applies incoming events to the local `DurableStore`.

```mermaid
flowchart TD
    RECV["gRPC client receives event"]
    SKIP_OWN{"event.DatacenterID\n== local DC?"}
    SKIP_APPLIED{"RaftIndex <=\nlastApplied[sourceDC]?"}
    APPLY["apply to DurableStore\n(Put or Delete)"]
    MARK["markApplied(event)"]
    SKIP["skip (drop)"]

    RECV --> SKIP_OWN
    SKIP_OWN -- yes --> SKIP
    SKIP_OWN -- no --> SKIP_APPLIED
    SKIP_APPLIED -- yes --> SKIP
    SKIP_APPLIED -- no --> APPLY
    APPLY --> MARK
```

**Idempotency:** The applier maintains `appliedIndices map[string]uint64` (source DC → last applied RaftIndex). Events with an index at or below the last applied index for that DC are skipped. This allows safe reconnect and replay without double-application.

**Loop prevention:** Events sourced from the local DC are always skipped.

### Conflict Resolution

`LWWConflictResolver` implements Last-Write-Wins:
1. Compare `Timestamp` fields of the local and remote events.
2. Remote wins if `remote.Timestamp > local.Timestamp`.
3. Tiebreaker: lexicographically greater `DatacenterID` wins (deterministic).

> **Active-Active note:** The current `applyPut` implementation in `ChangeApplier` applies the remote value unconditionally (overwrites). The `ConflictResolver` interface exists but is not yet called in `applyPut`. Full Active-Active conflict resolution using vector clocks is planned.

---

## Vector Clocks

`VectorClock` (`internal/replication/vectorclock.go`) is a standard Lamport vector clock keyed by datacenter ID.

### Clock Operations

| Method | Description |
|---|---|
| `Increment(dcID)` | Increment local clock for `dcID` |
| `Update(dcID, value)` | Set clock to `max(current, value)` for `dcID` |
| `Merge(other)` | Element-wise max of two clocks |
| `Get(dcID)` | Read current clock value for `dcID` |
| `Clone()` | Deep copy |
| `ToMap()` | Export as `map[string]uint64` for serialization |
| `Compare(other)` | Returns `ClockRelation` enum |

### Causality Relationships

`Compare` returns one of four `ClockRelation` values:

| Relation | Meaning |
|---|---|
| `ClockBefore` | This clock happened before the other |
| `ClockAfter` | This clock happened after the other |
| `ClockConcurrent` | Neither happened before the other (conflict) |
| `ClockEqual` | Clocks are identical |

### Vector Clock Update Sequence

```mermaid
sequenceDiagram
    participant DC1 as Datacenter 1\n(writer)
    participant DC2 as Datacenter 2\n(replica)
    participant DC3 as Datacenter 3\n(replica)

    Note over DC1: VC = {dc1:0, dc2:0, dc3:0}
    DC1->>DC1: write key-A\nIncrement(dc1) → VC={dc1:1}
    DC1->>DC2: replicate event\nVC={dc1:1}
    DC2->>DC2: Merge({dc1:1})\nVC={dc1:1, dc2:0}
    DC1->>DC1: write key-B\nIncrement(dc1) → VC={dc1:2}
    DC1->>DC3: replicate event\nVC={dc1:2}
    DC3->>DC3: Merge({dc1:2})\nVC={dc1:2, dc3:0}
    DC2->>DC1: replicate back (Active-Active scenario)\nVC={dc1:1, dc2:1}
    DC1->>DC1: Merge({dc1:1, dc2:1})\nVC={dc1:2, dc2:1}\ndc1:2 > dc1:1 → keep dc1
    Note over DC1,DC3: Compare dc1-VC={dc1:2,dc2:1} vs dc3-VC={dc1:2,dc3:1}\n→ ClockConcurrent (conflict)
```

---

## Topology Management

`TopologyManager` (`internal/replication/topology.go`) manages gRPC connections to all remote DCs configured as replication targets.

**Startup:**
1. For each `DatacenterTarget` in config, attempt to connect to each endpoint in order.
2. On connect, send a `Heartbeat` to verify the connection is live.
3. Mark DC healthy on success; log warning and continue on failure (startup is not aborted).

**Health checking:** A `time.Ticker` fires at `config.HealthCheck.Interval`. Each DC is checked concurrently (one goroutine per DC). A DC is marked unhealthy after `config.HealthCheck.FailureThreshold` consecutive failures.

> **Known limitation:** `connectDatacenter` currently uses `insecure.NewCredentials()`. TLS support for inter-DC gRPC connections is planned.

### Replication Topology Graph

```mermaid
graph TB
    subgraph "Primary DC (us-east-1)"
        P_RAFT["Raft Cluster\n(leader + followers)"]
        P_FSM["FSM.Apply()"]
        P_PUB["CDC Publisher"]
        P_SS["gRPC StreamServer"]
        P_RAFT --> P_FSM --> P_PUB --> P_SS
    end

    subgraph "Secondary DC 1 (eu-west-1)"
        S1_TM["TopologyManager"]
        S1_CA["ChangeApplier"]
        S1_RAFT["Local Raft\n(read serving)"]
        S1_TM -->|"gRPC StreamChanges"| P_SS
        S1_TM --> S1_CA --> S1_RAFT
    end

    subgraph "Secondary DC 2 (ap-southeast-1)"
        S2_TM["TopologyManager"]
        S2_CA["ChangeApplier"]
        S2_RAFT["Local Raft\n(read serving)"]
        S2_TM -->|"gRPC StreamChanges"| P_SS
        S2_TM --> S2_CA --> S2_RAFT
    end

    P_SS -.->|"heartbeat check"| S1_TM
    P_SS -.->|"heartbeat check"| S2_TM
```

---

## Lag Monitoring

`LagMonitor` (`internal/replication/lag_monitor.go`) tracks per-DC replication lag.

**Lag metrics per DC:**
- `LastReplicatedIndex` — last successfully applied Raft index from that DC
- `LastSuccess` — timestamp of the last successful replication event
- `LagEntries` — `leaderIndex - LastReplicatedIndex` (requires calling `CalculateLag`)
- `LagSeconds` — `now - LastSuccess` in seconds

`CheckLagThreshold(maxLagSeconds, maxLagEntries)` returns a slice of DC IDs that exceed either threshold and logs a warning for each.

### Lag Monitoring Flow

```mermaid
sequenceDiagram
    participant MON as LagMonitor
    participant APP as ChangeApplier
    participant SS as StreamServer (primary)

    loop every applied event
        APP->>MON: UpdateLag(dcID, lastIndex, timestamp)
        MON->>MON: update LagInfo{LastReplicatedIndex, LastSuccess}
    end

    loop periodic health check
        MON->>SS: GetReplicationStatus(ctx, req)
        SS-->>MON: ReplicationStatus{LastIndex: leaderIndex}
        MON->>MON: CalculateLag(dcID, leaderIndex)
        MON->>MON: LagEntries = leaderIndex - lastReplicated\nLagSeconds = now - lastSuccess
        MON->>MON: CheckLagThreshold(maxSec, maxEntries)
        alt lag exceeded
            MON->>MON: log warning
        end
    end
```

---

## End-to-End CDC Event Flow

```mermaid
sequenceDiagram
    participant Client as Client
    participant Leader as Raft Leader
    participant FSM as FSM.Apply()
    participant Pub as CDC Publisher
    participant Sub as Subscriber (stream-dc2)
    participant gRPC as gRPC StreamServer
    participant Remote as Remote DC Applier
    participant RStore as Remote DurableStore

    Client->>Leader: PUT /kv/foo = bar
    Leader->>Leader: replicate via Raft log
    Leader->>FSM: Apply(log entry)
    FSM->>FSM: write to DurableStore
    FSM->>Pub: Publish(ChangeEvent{op=put, key=foo, ...})
    Pub->>Sub: chan <- event.Clone() (non-blocking)
    gRPC->>Sub: Recv(ctx)
    Sub-->>gRPC: ChangeEvent
    gRPC->>Remote: stream.Send(pb.ChangeEvent)
    Remote->>Remote: ChangeApplier.Apply(event)
    Remote->>Remote: check: not own DC, not already applied
    Remote->>RStore: Put(ctx, "foo", "bar")
    Remote->>Remote: markApplied(event)
```

---

## gRPC API

The replication service is defined in `api/proto/replication.proto`.

| RPC | Description |
|---|---|
| `StreamChanges(StreamRequest)` | Server-streaming: sends `ChangeEvent` messages as they occur |
| `GetReplicationStatus(StatusRequest)` | Returns `ReplicationStatus` with per-DC lag info |
| `Heartbeat(HeartbeatRequest)` | Returns `HeartbeatResponse` for health checking |

**StreamRequest fields:**

| Field | Type | Description |
|---|---|---|
| `datacenter_id` | string | ID of the requesting DC |
| `from_index` | uint64 | Resume from this Raft index (events with index > from_index are sent) |
| `key_prefix` | string | Optional key prefix filter |

---

## Operational Considerations

- **Slow consumers:** If a secondary DC cannot consume events as fast as they are produced, its subscriber buffer (1000 events) will fill and events will be dropped. The dropped event count is observable via `Subscriber.Stats()`. The StreamServer logs a warning per drop. To recover, the secondary DC should reconnect with a `from_index` equal to its last successfully applied index.
- **Reconnect:** Because `StreamChanges` accepts a `FromIndex`, a secondary that disconnects can reconnect and resume from where it left off, provided the primary has not yet compacted its CDC event history.
- **Primary failover:** After a leader election, the new leader's FSM will resume publishing CDC events from the point it became leader. Secondary DCs should reconnect; their `from_index` ensures no duplicate application.
- **Metrics:** See `internal/replication/metrics.go` for Prometheus counters on events applied, skipped, and conflicts.

---

## See Also

- `docs/REPLICATION_MONITORING.md` — Grafana dashboards and alerting for replication lag
- `docs/ARCHITECTURE.md` — High-level system diagram
- `api/proto/replication.proto` — gRPC service definition
