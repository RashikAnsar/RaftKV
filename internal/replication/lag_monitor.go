package replication

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// LagMonitor tracks replication lag for each target datacenter
type LagMonitor struct {
	mu           sync.RWMutex
	datacenterID string
	logger       *zap.Logger

	// Per-datacenter lag tracking
	lagInfo map[string]*LagInfo
}

// LagInfo contains lag information for a datacenter
type LagInfo struct {
	DatacenterID        string
	LastReplicatedIndex uint64
	LastSuccess         time.Time
	LagEntries          uint64  // Number of entries behind
	LagSeconds          float64 // Estimated lag in seconds
}

// NewLagMonitor creates a new lag monitor
func NewLagMonitor(datacenterID string, logger *zap.Logger) *LagMonitor {
	return &LagMonitor{
		datacenterID: datacenterID,
		logger:       logger,
		lagInfo:      make(map[string]*LagInfo),
	}
}

// UpdateLag updates the lag information for a datacenter
func (lm *LagMonitor) UpdateLag(datacenterID string, lastIndex uint64, timestamp time.Time) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	info, exists := lm.lagInfo[datacenterID]
	if !exists {
		info = &LagInfo{
			DatacenterID: datacenterID,
		}
		lm.lagInfo[datacenterID] = info
	}

	// Update last replicated index
	prevIndex := info.LastReplicatedIndex
	info.LastReplicatedIndex = lastIndex
	info.LastSuccess = timestamp

	// Calculate lag in entries (this would need the current leader index)
	// For now, we'll track the difference from previous update
	if lastIndex > prevIndex {
		info.LagEntries = 0 // Caught up
	}

	// Calculate lag in seconds
	info.LagSeconds = time.Since(timestamp).Seconds()
}

// GetLag returns the lag information for a datacenter
func (lm *LagMonitor) GetLag(datacenterID string) *LagInfo {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	info, exists := lm.lagInfo[datacenterID]
	if !exists {
		return &LagInfo{
			DatacenterID: datacenterID,
			LagEntries:   0,
			LagSeconds:   0,
		}
	}

	// Return a copy to avoid race conditions
	return &LagInfo{
		DatacenterID:        info.DatacenterID,
		LastReplicatedIndex: info.LastReplicatedIndex,
		LastSuccess:         info.LastSuccess,
		LagEntries:          info.LagEntries,
		LagSeconds:          info.LagSeconds,
	}
}

// GetAllLags returns lag information for all datacenters
func (lm *LagMonitor) GetAllLags() map[string]*LagInfo {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	result := make(map[string]*LagInfo, len(lm.lagInfo))
	for dc, info := range lm.lagInfo {
		result[dc] = &LagInfo{
			DatacenterID:        info.DatacenterID,
			LastReplicatedIndex: info.LastReplicatedIndex,
			LastSuccess:         info.LastSuccess,
			LagEntries:          info.LagEntries,
			LagSeconds:          info.LagSeconds,
		}
	}
	return result
}

// CalculateLag calculates the current lag based on leader's index
func (lm *LagMonitor) CalculateLag(datacenterID string, leaderIndex uint64) *LagInfo {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	info, exists := lm.lagInfo[datacenterID]
	if !exists {
		info = &LagInfo{
			DatacenterID: datacenterID,
		}
		lm.lagInfo[datacenterID] = info
	}

	// Calculate lag in entries
	if leaderIndex > info.LastReplicatedIndex {
		info.LagEntries = leaderIndex - info.LastReplicatedIndex
	} else {
		info.LagEntries = 0
	}

	// Update lag in seconds based on last success
	if !info.LastSuccess.IsZero() {
		info.LagSeconds = time.Since(info.LastSuccess).Seconds()
	}

	return &LagInfo{
		DatacenterID:        info.DatacenterID,
		LastReplicatedIndex: info.LastReplicatedIndex,
		LastSuccess:         info.LastSuccess,
		LagEntries:          info.LagEntries,
		LagSeconds:          info.LagSeconds,
	}
}

// CheckLagThreshold checks if any datacenter exceeds the lag threshold
func (lm *LagMonitor) CheckLagThreshold(maxLagSeconds float64, maxLagEntries uint64) []string {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	exceeded := make([]string, 0)
	for dc, info := range lm.lagInfo {
		if info.LagSeconds > maxLagSeconds || info.LagEntries > maxLagEntries {
			exceeded = append(exceeded, dc)
			lm.logger.Warn("Replication lag threshold exceeded",
				zap.String("datacenter", dc),
				zap.Float64("lag_seconds", info.LagSeconds),
				zap.Uint64("lag_entries", info.LagEntries),
			)
		}
	}
	return exceeded
}

// Reset clears all lag information
func (lm *LagMonitor) Reset() {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.lagInfo = make(map[string]*LagInfo)
}
