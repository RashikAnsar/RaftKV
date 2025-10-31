package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestClientRedirectHandling tests automatic redirect following
func TestClientRedirectHandling(t *testing.T) {
	followerHits := 0
	leaderHits := 0

	// Mock leader server (processes request)
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaderHits++
		if r.URL.Path == "/keys/test" && r.Method == http.MethodPut {
			w.WriteHeader(http.StatusCreated)
		}
	}))
	defer leader.Close()

	// Mock follower server (returns 307 redirect)
	follower := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		followerHits++
		if r.URL.Path == "/keys/test" && r.Method == http.MethodPut {
			// Simulate follower redirecting to leader
			// Extract host from leader URL (remove http://)
			leaderHost := leader.URL[7:] // Remove "http://"
			w.Header().Set("X-Raft-Leader", leaderHost)
			w.Header().Set("Location", leader.URL+"/keys/test")
			w.WriteHeader(http.StatusTemporaryRedirect)
			w.Write([]byte(`{"error":"not leader"}`))
		}
	}))
	defer follower.Close()

	// Create client pointing to follower
	client := NewClient(Config{
		BaseURL: follower.URL,
		Timeout: 5 * time.Second,
	})

	// Perform PUT operation
	ctx := context.Background()
	err := client.Put(ctx, "test", []byte("value"))

	// Verify redirect was followed
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	if followerHits != 1 {
		t.Errorf("Expected 1 follower hit, got %d", followerHits)
	}

	if leaderHits != 1 {
		t.Errorf("Expected 1 leader hit, got %d", leaderHits)
	}

	// Verify leader is cached
	expectedLeader := "http://" + leader.URL[7:]
	if client.GetLeaderURL() != expectedLeader {
		t.Errorf("Expected cached leader %s, got %s", expectedLeader, client.GetLeaderURL())
	}
}

// TestClientLeaderCaching tests that leader is cached after first redirect
func TestClientLeaderCaching(t *testing.T) {
	client := NewClient(Config{
		BaseURL: "http://localhost:8082",
		Timeout: 5 * time.Second,
	})

	// Initially, leader URL should match base URL
	if client.GetLeaderURL() != "http://localhost:8082" {
		t.Errorf("Expected initial leader URL to be %s, got %s",
			"http://localhost:8082", client.GetLeaderURL())
	}

	// Simulate caching a different leader
	client.SetLeaderURL("http://localhost:8081")

	if client.GetLeaderURL() != "http://localhost:8081" {
		t.Errorf("Expected cached leader URL to be %s, got %s",
			"http://localhost:8081", client.GetLeaderURL())
	}

	// Reset cache
	client.ResetLeaderCache()

	if client.GetLeaderURL() != "http://localhost:8082" {
		t.Errorf("Expected reset leader URL to be %s, got %s",
			"http://localhost:8082", client.GetLeaderURL())
	}
}

// TestClientMaxRedirects tests that client respects max redirects
func TestClientMaxRedirects(t *testing.T) {
	redirectCount := 0

	// Mock server that always redirects (simulating no reachable leader)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectCount++
		// Always redirect to an unreachable address
		w.Header().Set("X-Raft-Leader", "unreachable:9999")
		w.WriteHeader(http.StatusTemporaryRedirect)
		w.Write([]byte(`{"error":"not leader"}`))
	}))
	defer server.Close()

	client := NewClient(Config{
		BaseURL:      server.URL,
		Timeout:      2 * time.Second,
		MaxRedirects: 3,
	})

	// This should fail after trying the redirects
	ctx := context.Background()
	err := client.Put(ctx, "test", []byte("value"))

	if err == nil {
		t.Fatal("Expected error due to unreachable leader")
	}

	// Should have tried at least once (may fail on connection to unreachable host)
	if redirectCount < 1 {
		t.Errorf("Expected at least 1 redirect attempt, got %d", redirectCount)
	}
}

// TestClientGet tests GET operations (no redirects for reads)
func TestClientGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/keys/test" && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("test-value"))
		}
	}))
	defer server.Close()

	client := NewClient(Config{
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	})

	ctx := context.Background()
	value, err := client.Get(ctx, "test")

	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}

	if string(value) != "test-value" {
		t.Errorf("Expected 'test-value', got '%s'", string(value))
	}
}

// TestClientDelete tests DELETE operations with redirect
func TestClientDelete(t *testing.T) {
	followerHits := 0
	leaderHits := 0

	// Mock leader server
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaderHits++
		if r.URL.Path == "/keys/test" && r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer leader.Close()

	// Mock follower server
	follower := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		followerHits++
		if r.URL.Path == "/keys/test" && r.Method == http.MethodDelete {
			leaderHost := leader.URL[7:]
			w.Header().Set("X-Raft-Leader", leaderHost)
			w.WriteHeader(http.StatusTemporaryRedirect)
		}
	}))
	defer follower.Close()

	client := NewClient(Config{
		BaseURL: follower.URL,
		Timeout: 5 * time.Second,
	})

	ctx := context.Background()
	err := client.Delete(ctx, "test")

	if err != nil {
		t.Fatalf("DELETE failed: %v", err)
	}

	if followerHits != 1 {
		t.Errorf("Expected 1 follower hit, got %d", followerHits)
	}

	if leaderHits != 1 {
		t.Errorf("Expected 1 leader hit, got %d", leaderHits)
	}
}
