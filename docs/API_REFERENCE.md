# RaftKV API Reference

> Complete reference for HTTP REST and gRPC APIs

## Table of Contents

- [Overview](#overview)
- [Authentication](#authentication)
- [HTTP REST API](#http-rest-api)
  - [Key-Value Operations](#key-value-operations)
  - [Cluster Management](#cluster-management)
  - [Admin Endpoints](#admin-endpoints)
  - [Health & Monitoring](#health--monitoring)
  - [Authentication Endpoints](#authentication-endpoints)
  - [User Management](#user-management)
  - [API Key Management](#api-key-management)
- [gRPC API](#grpc-api)
- [Error Handling](#error-handling)

---

## Overview

RaftKV provides both HTTP REST and gRPC APIs for distributed key-value storage with Raft consensus.

**Base Configuration:**
- HTTP Default Port: `8080`
- gRPC Default Port: `9090`
- Max Request Size: `1MB` (configurable via `performance.max_request_size`)
- Request Timeout: `10s` read, `10s` write (configurable)
- Rate Limiting: Optional (configurable via `performance.enable_rate_limit`)

**Operating Modes:**
- **Single-node mode**: Direct storage operations
- **Cluster mode**: Raft-based distributed consensus with leader forwarding

---

## Authentication

When authentication is enabled (`auth.enabled: true`), most endpoints require authentication.

### Authentication Methods

**1. JWT Bearer Token:**
```bash
curl -H "Authorization: Bearer eyJhbGci..." http://localhost:8080/keys/foo
```

**2. API Key:**
```bash
curl -H "X-API-Key: raftkv_1234567890abcdef" http://localhost:8080/keys/foo
```

### Authorization Roles (RBAC)

| Role | Permissions |
|------|-------------|
| **admin** | Full access: read, write, user management, cluster operations |
| **write** | Read and write key-value operations |
| **read** | Read-only key-value operations |

### Default Admin User

On first startup, RaftKV creates a default admin user:
- **Username**: `admin`
- **Password**: `admin`
- **⚠️ Change immediately in production!**

---

## HTTP REST API

### Key-Value Operations

#### Get Value

Retrieve a value by key.

**Request:**
```http
GET /keys/{key}
```

**Authentication:** Required if enabled (read permission)

**Response:** `200 OK`
```
Content-Type: application/octet-stream

[binary data]
```

**Error Responses:**
- `400 Bad Request` - Key is required
- `404 Not Found` - Key not found
- `401 Unauthorized` - Authentication required/failed
- `500 Internal Server Error` - Storage error

**Example:**
```bash
# Get value
curl http://localhost:8080/keys/user:123 \
  -H "Authorization: Bearer $TOKEN"

# Response: John Doe
```

---

#### Put Value

Store or update a key-value pair.

**Request:**
```http
PUT /keys/{key}
Content-Type: application/octet-stream

[binary data]
```

**Authentication:** Required if enabled (write permission)

**Response:** `201 Created`

**Cluster Mode Responses:**
- `307 Temporary Redirect` - Not leader, redirect to leader (Location header)
- `503 Service Unavailable` - No leader elected

**Example:**
```bash
# Put value
curl -X PUT http://localhost:8080/keys/user:123 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/octet-stream" \
  --data-binary "John Doe"

# Put JSON data
curl -X PUT http://localhost:8080/keys/config:app \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-binary '{"theme":"dark","lang":"en"}'

# Put file
curl -X PUT http://localhost:8080/keys/file:logo \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary @logo.png
```

---

#### Delete Key

Remove a key-value pair.

**Request:**
```http
DELETE /keys/{key}
```

**Authentication:** Required if enabled (write permission)

**Response:** `204 No Content`

**Cluster Mode Responses:**
- `307 Temporary Redirect` - Not leader
- `503 Service Unavailable` - No leader elected

**Example:**
```bash
curl -X DELETE http://localhost:8080/keys/user:123 \
  -H "Authorization: Bearer $TOKEN"
```

---

#### List Keys

List keys with optional prefix filtering.

**Request:**
```http
GET /keys?prefix={prefix}&limit={limit}
```

**Query Parameters:**
- `prefix` (string, optional) - Filter keys by prefix
- `limit` (int, optional) - Max keys to return (default: 1000)

**Authentication:** Required if enabled (read permission)

**Response:** `200 OK`

Single-node mode:
```json
{
  "keys": ["user:123", "user:456", "config:app"],
  "count": 3,
  "prefix": "user:",
  "limit": 1000
}
```

Cluster mode:
```json
{
  "keys": ["user:123", "user:456"],
  "count": 2,
  "prefix": "user:",
  "limit": 1000,
  "leader": "node1:8080",
  "state": "Follower"
}
```

**Example:**
```bash
# List all keys
curl http://localhost:8080/keys \
  -H "Authorization: Bearer $TOKEN"

# List keys with prefix
curl "http://localhost:8080/keys?prefix=user:" \
  -H "Authorization: Bearer $TOKEN"

# List with limit
curl "http://localhost:8080/keys?prefix=config:&limit=10" \
  -H "Authorization: Bearer $TOKEN"
```

---

### Cluster Management

**Note:** Cluster management endpoints are only available in Raft mode (`raft.enabled: true`).

#### Join Cluster

Add a node to the cluster.

**Request:**
```http
POST /cluster/join
Content-Type: application/json

{
  "node_id": "node2",
  "addr": "node2:7000"
}
```

**Authentication:** Not required (internal cluster operation)

**Response:** `200 OK`
```json
{
  "message": "node added successfully",
  "node_id": "node2",
  "addr": "node2:7000"
}
```

**Error Responses:**
- `307 Temporary Redirect` - Not leader
- `400 Bad Request` - Invalid request body
- `500 Internal Server Error` - Failed to add node

**Example:**
```bash
# New node joining cluster
curl -X POST http://node1:8080/cluster/join \
  -H "Content-Type: application/json" \
  -d '{"node_id": "node2", "addr": "node2:7000"}'
```

**Usage:** Typically called automatically when a node starts with `raft.join_addr` configured.

---

#### Remove Node

Remove a node from the cluster.

**Request:**
```http
POST /cluster/remove
Content-Type: application/json

{
  "node_id": "node2"
}
```

**Authentication:** Not required

**Response:** `200 OK`
```json
{
  "message": "node removed successfully",
  "node_id": "node2"
}
```

**Error Responses:**
- `307 Temporary Redirect` - Not leader
- `400 Bad Request` - Invalid request body
- `500 Internal Server Error` - Failed to remove node

**Example:**
```bash
curl -X POST http://node1:8080/cluster/remove \
  -H "Content-Type: application/json" \
  -d '{"node_id": "node2"}'
```

---

#### List Nodes

Get all nodes in the cluster.

**Request:**
```http
GET /cluster/nodes
```

**Authentication:** Not required

**Response:** `200 OK`
```json
{
  "nodes": [
    {
      "id": "node1",
      "address": "node1:7000",
      "suffrage": "Voter"
    },
    {
      "id": "node2",
      "address": "node2:7000",
      "suffrage": "Voter"
    },
    {
      "id": "node3",
      "address": "node3:7000",
      "suffrage": "Voter"
    }
  ],
  "count": 3
}
```

**Suffrage Types:**
- `Voter` - Participates in leader election and log replication
- `Nonvoter` - Receives log entries but doesn't vote (future feature)

**Example:**
```bash
curl http://localhost:8080/cluster/nodes | jq
```

---

#### Get Leader

Get current Raft leader information.

**Request:**
```http
GET /cluster/leader
```

**Authentication:** Not required

**Response:** `200 OK`
```json
{
  "leader": "node1:8080",
  "state": "Follower"
}
```

**Possible States:**
- `Leader` - This node is the leader
- `Follower` - This node is a follower
- `Candidate` - Election in progress

**Example:**
```bash
# Check who is leader
curl http://localhost:8080/cluster/leader

# Check if this node is leader
curl http://localhost:8080/cluster/leader | jq -r '.state'
```

---

### Admin Endpoints

#### Create Snapshot

Manually trigger a snapshot creation.

**Request:**
```http
POST /admin/snapshot
```

**Authentication:** Required if enabled (admin permission)

**Response:** `200 OK`

Single-node mode:
```json
{
  "snapshot": "/data/snapshots/snapshot-1234.gob",
  "message": "snapshot created successfully"
}
```

Cluster mode:
```json
{
  "message": "snapshot created successfully"
}
```

**Example:**
```bash
# Trigger snapshot
curl -X POST http://localhost:8080/admin/snapshot \
  -H "Authorization: Bearer $TOKEN"
```

**Use Cases:**
- Manual backup before maintenance
- Force WAL compaction
- Reduce recovery time

---

### Health & Monitoring

#### Health Check

Check if the service is healthy.

**Request:**
```http
GET /health
```

**Authentication:** Not required

**Response:** `200 OK`

Single-node mode:
```json
{
  "status": "healthy"
}
```

Cluster mode:
```json
{
  "status": "healthy",
  "state": "Leader",
  "leader": "node1:8080"
}
```

**Example:**
```bash
# Kubernetes liveness probe
curl http://localhost:8080/health
```

---

#### Readiness Check

Check if the service is ready to accept traffic.

**Request:**
```http
GET /ready
```

**Authentication:** Not required

**Response:** `200 OK`

Single-node mode:
```json
{
  "status": "ready"
}
```

Cluster mode:
```json
{
  "status": "ready",
  "state": "Follower",
  "leader": "node1:8080"
}
```

**Error Responses:**
- `503 Service Unavailable` - Store not initialized or no leader

**Example:**
```bash
# Kubernetes readiness probe
curl http://localhost:8080/ready
```

---

#### Statistics

Get store and cluster statistics.

**Request:**
```http
GET /stats
```

**Authentication:** Required if enabled (read permission)

**Response:** `200 OK`

Single-node mode:
```json
{
  "gets": 1234,
  "puts": 567,
  "deletes": 89,
  "key_count": 1000,
  "cache": {
    "hits": 5000,
    "misses": 1000,
    "hit_rate": 0.833,
    "size": 500,
    "evictions": 100
  }
}
```

Cluster mode:
```json
{
  "store": {
    "gets": 1234,
    "puts": 567,
    "deletes": 89,
    "key_count": 1000
  },
  "raft": {
    "state": "Leader",
    "term": "5",
    "last_log_index": "1234",
    "commit_index": "1234",
    "applied_index": "1234",
    "num_peers": "2"
  }
}
```

**Example:**
```bash
curl http://localhost:8080/stats \
  -H "Authorization: Bearer $TOKEN" | jq
```

---

#### Prometheus Metrics

Expose Prometheus-formatted metrics.

**Request:**
```http
GET /metrics
```

**Authentication:** Not required

**Response:** `200 OK`
```
Content-Type: text/plain

# HELP kvstore_http_requests_total Total number of HTTP requests
# TYPE kvstore_http_requests_total counter
kvstore_http_requests_total{method="GET",endpoint="/keys",status="200"} 1234

# HELP kvstore_storage_operations_total Total number of storage operations
# TYPE kvstore_storage_operations_total counter
kvstore_storage_operations_total{operation="get",status="success"} 5678

# HELP kvstore_storage_keys_total Current number of keys
# TYPE kvstore_storage_keys_total gauge
kvstore_storage_keys_total 1000
```

**Key Metrics:**
- `kvstore_http_requests_total` - HTTP request count
- `kvstore_http_request_duration_seconds` - HTTP latency histogram
- `kvstore_storage_operations_total` - Storage operation count
- `kvstore_storage_operation_duration_seconds` - Storage latency histogram
- `kvstore_storage_keys_total` - Current key count
- `raftkv_wal_segments_total` - WAL segment count
- `raftkv_wal_size_bytes` - WAL disk usage
- `raftkv_raft_log_entries_total` - Raft log entry count

**Example:**
```bash
# Scrape metrics
curl http://localhost:8080/metrics

# Prometheus scrape config
scrape_configs:
  - job_name: 'raftkv'
    static_configs:
      - targets: ['localhost:8080']
```

---

#### Root Endpoint

Get service information and available endpoints.

**Request:**
```http
GET /
```

**Authentication:** Not required

**Response:** `200 OK`
```json
{
  "service": "RaftKV",
  "version": "1.0.0",
  "endpoints": {
    "GET /keys/{key}": "Get value",
    "PUT /keys/{key}": "Put value",
    "DELETE /keys/{key}": "Delete key",
    "GET /keys?prefix=": "List keys",
    "POST /admin/snapshot": "Create snapshot",
    "GET /health": "Health check",
    "GET /ready": "Readiness check",
    "GET /stats": "Statistics",
    "GET /metrics": "Prometheus metrics"
  }
}
```

**Example:**
```bash
curl http://localhost:8080/
```

---

### Authentication Endpoints

#### Login

Authenticate and obtain a JWT token.

**Request:**
```http
POST /auth/login
Content-Type: application/json

{
  "username": "admin",
  "password": "admin"
}
```

**Authentication:** Not required

**Response:** `200 OK`
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_at": "2025-11-09T13:00:00Z",
  "user": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "username": "admin",
    "role": "admin",
    "created_at": "2025-11-09T10:00:00Z",
    "updated_at": "2025-11-09T10:00:00Z",
    "disabled": false
  }
}
```

**Error Responses:**
- `400 Bad Request` - Invalid request body
- `401 Unauthorized` - Invalid credentials

**Example:**
```bash
# Login and save token
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin"}' \
  | jq -r '.token')

# Use token
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/keys/foo
```

---

#### Refresh Token

Refresh an existing JWT token.

**Request:**
```http
POST /auth/refresh
```

**Authentication:** Required (JWT only)

**Response:** `200 OK`
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_at": "2025-11-09T14:00:00Z",
  "user": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "username": "admin",
    "role": "admin",
    "created_at": "2025-11-09T10:00:00Z",
    "updated_at": "2025-11-09T10:00:00Z",
    "disabled": false
  }
}
```

**Error Responses:**
- `400 Bad Request` - JWT token required
- `404 Not Found` - User not found

**Example:**
```bash
# Refresh token
NEW_TOKEN=$(curl -s -X POST http://localhost:8080/auth/refresh \
  -H "Authorization: Bearer $TOKEN" \
  | jq -r '.token')
```

---

#### Logout

Logout (informational only, client should discard token).

**Request:**
```http
POST /auth/logout
```

**Authentication:** Required

**Response:** `200 OK`
```json
{
  "message": "Logged out successfully"
}
```

**Note:** JWT tokens are stateless. Logout is informational; clients should discard the token.

**Example:**
```bash
curl -X POST http://localhost:8080/auth/logout \
  -H "Authorization: Bearer $TOKEN"
```

---

### User Management

**Note:** All user management endpoints require admin role.

#### Create User

Create a new user.

**Request:**
```http
POST /auth/users
Content-Type: application/json

{
  "username": "newuser",
  "password": "password123",
  "role": "write"
}
```

**Authentication:** Required (admin permission)

**Response:** `201 Created`
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440001",
  "username": "newuser",
  "role": "write",
  "created_at": "2025-11-09T10:00:00Z",
  "updated_at": "2025-11-09T10:00:00Z",
  "disabled": false
}
```

**Validation:**
- Username: 3-50 characters, alphanumeric + underscore
- Password: Minimum 8 characters
- Role: `admin`, `write`, or `read`

**Error Responses:**
- `400 Bad Request` - Invalid request or username exists
- `403 Forbidden` - Not admin

**Example:**
```bash
curl -X POST http://localhost:8080/auth/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "alice",
    "password": "securepass123",
    "role": "write"
  }'
```

---

#### List Users

List all users.

**Request:**
```http
GET /auth/users
```

**Authentication:** Required (admin permission)

**Response:** `200 OK`
```json
[
  {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "username": "admin",
    "role": "admin",
    "created_at": "2025-11-09T10:00:00Z",
    "updated_at": "2025-11-09T10:00:00Z",
    "disabled": false
  },
  {
    "id": "550e8400-e29b-41d4-a716-446655440001",
    "username": "alice",
    "role": "write",
    "created_at": "2025-11-09T11:00:00Z",
    "updated_at": "2025-11-09T11:00:00Z",
    "disabled": false
  }
]
```

**Example:**
```bash
curl http://localhost:8080/auth/users \
  -H "Authorization: Bearer $TOKEN" | jq
```

---

#### Get User

Get a specific user by ID.

**Request:**
```http
GET /auth/users/{id}
```

**Authentication:** Required (admin permission)

**Response:** `200 OK`
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440001",
  "username": "alice",
  "role": "write",
  "created_at": "2025-11-09T11:00:00Z",
  "updated_at": "2025-11-09T11:00:00Z",
  "disabled": false
}
```

**Error Responses:**
- `404 Not Found` - User not found

**Example:**
```bash
curl http://localhost:8080/auth/users/550e8400-e29b-41d4-a716-446655440001 \
  -H "Authorization: Bearer $TOKEN"
```

---

#### Delete User

Delete a user.

**Request:**
```http
DELETE /auth/users/{id}
```

**Authentication:** Required (admin permission)

**Response:** `204 No Content`

**Example:**
```bash
curl -X DELETE http://localhost:8080/auth/users/550e8400-e29b-41d4-a716-446655440001 \
  -H "Authorization: Bearer $TOKEN"
```

---

#### Update User Role

Change a user's role.

**Request:**
```http
PUT /auth/users/{id}/role
Content-Type: application/json

{
  "role": "admin"
}
```

**Authentication:** Required (admin permission)

**Response:** `200 OK`
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440001",
  "username": "alice",
  "role": "admin",
  "created_at": "2025-11-09T11:00:00Z",
  "updated_at": "2025-11-09T12:00:00Z",
  "disabled": false
}
```

**Example:**
```bash
curl -X PUT http://localhost:8080/auth/users/550e8400.../role \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"role": "admin"}'
```

---

#### Disable/Enable User

Disable or enable a user account.

**Disable:**
```http
POST /auth/users/{id}/disable
```

**Enable:**
```http
POST /auth/users/{id}/enable
```

**Authentication:** Required (admin permission)

**Response:** `200 OK`
```json
{
  "message": "User disabled"
}
```

**Example:**
```bash
# Disable user
curl -X POST http://localhost:8080/auth/users/550e8400.../disable \
  -H "Authorization: Bearer $TOKEN"

# Enable user
curl -X POST http://localhost:8080/auth/users/550e8400.../enable \
  -H "Authorization: Bearer $TOKEN"
```

---

### API Key Management

#### Create API Key

Generate a new API key.

**Request:**
```http
POST /auth/api-keys
Content-Type: application/json

{
  "name": "Production API Key",
  "role": "write",
  "expires_in": "720h"
}
```

**Fields:**
- `name` (required) - Descriptive name for the key
- `role` (optional) - Role to assign (defaults to user's role)
- `expires_in` (optional) - Duration string (e.g., "720h", "30d")

**Authentication:** Required

**Response:** `201 Created`
```json
{
  "id": "key-550e8400-e29b-41d4-a716-446655440000",
  "key": "raftkv_1234567890abcdef1234567890abcdef",
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "Production API Key",
  "role": "write",
  "created_at": "2025-11-09T10:00:00Z",
  "updated_at": "2025-11-09T10:00:00Z",
  "expires_at": "2025-12-09T10:00:00Z",
  "disabled": false
}
```

**⚠️ Important:** The `key` field is only returned on creation. Store it securely!

**Example:**
```bash
# Create API key
API_KEY=$(curl -s -X POST http://localhost:8080/auth/api-keys \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Production Key",
    "role": "write",
    "expires_in": "720h"
  }' | jq -r '.key')

# Use API key
curl -H "X-API-Key: $API_KEY" http://localhost:8080/keys/foo
```

---

#### List API Keys

List all API keys.

**Request:**
```http
GET /auth/api-keys
```

**Authentication:** Required

**Response:** `200 OK`
```json
[
  {
    "id": "key-550e8400-e29b-41d4-a716-446655440000",
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "Production API Key",
    "role": "write",
    "created_at": "2025-11-09T10:00:00Z",
    "updated_at": "2025-11-09T10:00:00Z",
    "expires_at": "2025-12-09T10:00:00Z",
    "last_used": "2025-11-09T11:30:00Z",
    "disabled": false
  }
]
```

**Note:** Regular users see only their own keys. Admins see all keys.

**Example:**
```bash
curl http://localhost:8080/auth/api-keys \
  -H "Authorization: Bearer $TOKEN" | jq
```

---

#### Get API Key

Get a specific API key by ID.

**Request:**
```http
GET /auth/api-keys/{id}
```

**Authentication:** Required

**Response:** `200 OK`
```json
{
  "id": "key-550e8400-e29b-41d4-a716-446655440000",
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "Production API Key",
  "role": "write",
  "created_at": "2025-11-09T10:00:00Z",
  "updated_at": "2025-11-09T10:00:00Z",
  "expires_at": "2025-12-09T10:00:00Z",
  "last_used": "2025-11-09T11:30:00Z",
  "disabled": false
}
```

**Error Responses:**
- `403 Forbidden` - User can only view their own keys
- `404 Not Found` - API key not found

**Example:**
```bash
curl http://localhost:8080/auth/api-keys/key-550e8400... \
  -H "Authorization: Bearer $TOKEN"
```

---

#### Revoke API Key

Revoke (delete) an API key.

**Request:**
```http
DELETE /auth/api-keys/{id}
```

**Authentication:** Required

**Response:** `204 No Content`

**Error Responses:**
- `403 Forbidden` - User can only revoke their own keys
- `404 Not Found` - API key not found

**Example:**
```bash
curl -X DELETE http://localhost:8080/auth/api-keys/key-550e8400... \
  -H "Authorization: Bearer $TOKEN"
```

---

### Self-Service Endpoints

#### Get Current User

Get information about the authenticated user.

**Request:**
```http
GET /auth/me
```

**Authentication:** Required

**Response:** `200 OK`
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "username": "alice",
  "role": "write",
  "created_at": "2025-11-09T10:00:00Z",
  "updated_at": "2025-11-09T10:00:00Z",
  "disabled": false
}
```

**Example:**
```bash
curl http://localhost:8080/auth/me \
  -H "Authorization: Bearer $TOKEN"
```

---

#### Change Password

Change the authenticated user's password.

**Request:**
```http
PUT /auth/password
Content-Type: application/json

{
  "old_password": "oldpass123",
  "new_password": "newpass456"
}
```

**Authentication:** Required

**Response:** `200 OK`
```json
{
  "message": "Password changed successfully"
}
```

**Validation:**
- New password: Minimum 8 characters
- Old password must match current password

**Error Responses:**
- `400 Bad Request` - Invalid passwords
- `401 Unauthorized` - Old password incorrect

**Example:**
```bash
curl -X PUT http://localhost:8080/auth/password \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "old_password": "admin",
    "new_password": "newsecurepass123"
  }'
```

---

## gRPC API

### Overview

**Service:** `kvstore.KVStore`
**Default Port:** `9090`
**Protocol:** gRPC (HTTP/2)
**TLS:** Optional (configurable via `tls.enabled`)
**Authentication:** Not implemented (use HTTP API for authenticated access)

### Proto Definitions

Location: [api/proto/kv.proto](../api/proto/kv.proto)

### Methods

#### Get

Retrieve a value by key.

**Request:**
```protobuf
message GetRequest {
  string key = 1;
  bool consistent = 2;  // If true, read from leader (linearizable)
}
```

**Response:**
```protobuf
message GetResponse {
  bytes value = 1;
  bool found = 2;
  bool from_leader = 3;  // Indicates if read was from leader
}
```

**Error Codes:**
- `INVALID_ARGUMENT` - Key is empty
- `FAILED_PRECONDITION` - Consistent read requires leader
- `INTERNAL` - Internal error

**Example (Go):**
```go
resp, err := client.Get(ctx, &pb.GetRequest{
    Key:        "user:123",
    Consistent: true,  // Linearizable read
})
if err != nil {
    log.Fatal(err)
}
if resp.Found {
    fmt.Println("Value:", string(resp.Value))
}
```

**Example (grpcurl):**
```bash
grpcurl -plaintext \
  -d '{"key": "user:123", "consistent": true}' \
  localhost:9090 kvstore.KVStore/Get
```

---

#### Put

Store a key-value pair.

**Request:**
```protobuf
message PutRequest {
  string key = 1;
  bytes value = 2;
}
```

**Response:**
```protobuf
message PutResponse {
  bool success = 1;
  string error = 2;
  string leader = 3;  // Leader address (if applicable)
}
```

**Error Codes:**
- `INVALID_ARGUMENT` - Key is empty
- `FAILED_PRECONDITION` - Not the leader
- `INTERNAL` - Failed to apply command

**Example (Go):**
```go
resp, err := client.Put(ctx, &pb.PutRequest{
    Key:   "user:123",
    Value: []byte("John Doe"),
})
if err != nil {
    log.Fatal(err)
}
```

**Example (grpcurl):**
```bash
grpcurl -plaintext \
  -d '{"key": "user:123", "value": "Sm9obiBEb2U="}' \
  localhost:9090 kvstore.KVStore/Put
```

---

#### Delete

Remove a key-value pair.

**Request:**
```protobuf
message DeleteRequest {
  string key = 1;
}
```

**Response:**
```protobuf
message DeleteResponse {
  bool success = 1;
  string error = 2;
  string leader = 3;
}
```

**Error Codes:**
- `INVALID_ARGUMENT` - Key is empty
- `FAILED_PRECONDITION` - Not the leader
- `INTERNAL` - Failed to apply command

**Example (Go):**
```go
resp, err := client.Delete(ctx, &pb.DeleteRequest{
    Key: "user:123",
})
```

**Example (grpcurl):**
```bash
grpcurl -plaintext \
  -d '{"key": "user:123"}' \
  localhost:9090 kvstore.KVStore/Delete
```

---

#### List

List keys matching a prefix.

**Request:**
```protobuf
message ListRequest {
  string prefix = 1;
  int32 limit = 2;  // 0 = no limit
}
```

**Response:**
```protobuf
message ListResponse {
  repeated string keys = 1;
  int32 total = 2;  // Total matching keys
}
```

**Error Codes:**
- `INTERNAL` - Failed to list keys

**Example (Go):**
```go
resp, err := client.List(ctx, &pb.ListRequest{
    Prefix: "user:",
    Limit:  100,
})
if err != nil {
    log.Fatal(err)
}
for _, key := range resp.Keys {
    fmt.Println(key)
}
```

**Example (grpcurl):**
```bash
grpcurl -plaintext \
  -d '{"prefix": "user:", "limit": 100}' \
  localhost:9090 kvstore.KVStore/List
```

---

#### GetStats

Get store and Raft statistics.

**Request:**
```protobuf
message StatsRequest {}
```

**Response:**
```protobuf
message StatsResponse {
  uint64 key_count = 1;
  uint64 get_count = 2;
  uint64 put_count = 3;
  uint64 delete_count = 4;
  string raft_state = 5;      // "Leader", "Follower", "Candidate"
  string raft_leader = 6;
  uint64 raft_term = 7;
  uint64 raft_last_index = 8;
}
```

**Example (grpcurl):**
```bash
grpcurl -plaintext \
  localhost:9090 kvstore.KVStore/GetStats
```

---

#### GetLeader

Get current Raft leader information.

**Request:**
```protobuf
message LeaderRequest {}
```

**Response:**
```protobuf
message LeaderResponse {
  string leader_id = 1;
  string leader_address = 2;
  bool is_leader = 3;
}
```

**Example (grpcurl):**
```bash
grpcurl -plaintext \
  localhost:9090 kvstore.KVStore/GetLeader
```

---

## Error Handling

### HTTP Error Response Format

All HTTP errors return JSON:

```json
{
  "error": "error message description"
}
```

### Common HTTP Status Codes

| Code | Status | Description |
|------|--------|-------------|
| **200** | OK | Request successful |
| **201** | Created | Resource created successfully |
| **204** | No Content | Request successful, no content |
| **307** | Temporary Redirect | Redirect to leader (cluster mode) |
| **400** | Bad Request | Invalid request parameters |
| **401** | Unauthorized | Authentication required/failed |
| **403** | Forbidden | Insufficient permissions |
| **404** | Not Found | Resource not found |
| **500** | Internal Server Error | Server error |
| **503** | Service Unavailable | Service temporarily unavailable |

### gRPC Error Codes

| Code | Description |
|------|-------------|
| **OK** | Success |
| **INVALID_ARGUMENT** | Invalid request parameters |
| **FAILED_PRECONDITION** | Operation requires specific state (e.g., leader) |
| **NOT_FOUND** | Resource not found |
| **INTERNAL** | Internal server error |
| **UNAVAILABLE** | Service unavailable |
| **UNAUTHENTICATED** | Authentication failed (future) |
| **PERMISSION_DENIED** | Insufficient permissions (future) |

### Leader Redirection (Cluster Mode)

When a write request is sent to a non-leader node:

**HTTP:**
```http
HTTP/1.1 307 Temporary Redirect
Location: http://node1:8080/keys/foo
X-Raft-Leader: node1:8080
```

**gRPC:**
```go
resp.Success = false
resp.Error = "not leader"
resp.Leader = "node1:9090"
```

**Client Handling:**
- HTTP clients should follow the redirect automatically
- gRPC clients should retry the request to the leader

---

## Middleware & Features

### Automatic Middleware (Applied to All Requests)

1. **Recovery** - Panic recovery with error logging
2. **Logging** - Request/response logging with request IDs
3. **Metrics** - Prometheus metrics collection
4. **CORS** - Cross-origin resource sharing support
5. **Rate Limiting** - Optional requests-per-second limiting

### Request/Response Features

- **Request ID**: Unique UUID generated per request (`X-Request-ID` header)
- **Request Size Limiting**: 1MB default (configurable)
- **Timeout Handling**: 10s read, 10s write (configurable)
- **Structured Logging**: Zap-based structured logging
- **Context Propagation**: Request context passed through all layers

---

## Configuration

### HTTP Server Configuration

```yaml
server:
  http_addr: ":8080"
  grpc_addr: ":9090"
  enable_grpc: true

performance:
  read_timeout: 10s
  write_timeout: 10s
  max_request_size: 1048576  # 1MB
  enable_rate_limit: false
  rate_limit: 1000  # requests per second
```

### Authentication Configuration

```yaml
auth:
  enabled: true
  jwt_secret: "${JWT_SECRET}"
  token_expiry: 1h
```

### TLS Configuration

```yaml
tls:
  enabled: true
  cert_file: "certs/server-cert.pem"
  key_file: "certs/server-key.pem"
  ca_file: "certs/ca-cert.pem"
  enable_mtls: false
```

---

## Client Libraries

### HTTP Client (Go)

```go
import "github.com/RashikAnsar/raftkv/pkg/client"

client := client.NewHTTPClient("http://localhost:8080", &client.Config{
    Token: "eyJhbGci...",
})

// Put value
err := client.Put(ctx, "user:123", []byte("John Doe"))

// Get value
value, err := client.Get(ctx, "user:123")

// Delete key
err := client.Delete(ctx, "user:123")

// List keys
keys, err := client.List(ctx, "user:", 100)
```

### gRPC Client (Go)

```go
import (
    "google.golang.org/grpc"
    pb "github.com/RashikAnsar/raftkv/api/proto"
)

conn, err := grpc.Dial("localhost:9090", grpc.WithInsecure())
defer conn.Close()

client := pb.NewKVStoreClient(conn)

// Put value
resp, err := client.Put(ctx, &pb.PutRequest{
    Key:   "user:123",
    Value: []byte("John Doe"),
})

// Get value
resp, err := client.Get(ctx, &pb.GetRequest{
    Key:        "user:123",
    Consistent: true,
})
```

---

## Further Reading

- [ARCHITECTURE.md](ARCHITECTURE.md) - Complete system architecture
- [DEPLOYMENT.md](DEPLOYMENT.md) - Deployment guides
- [OPERATIONS.md](OPERATIONS.md) - Operations runbook
- [config/README.md](../config/README.md) - Configuration reference

