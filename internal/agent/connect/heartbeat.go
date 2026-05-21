package connect

import (
	"context"
	"log"
	"runtime"
	"time"

	"vaultfleet/pkg/protocol"
)

const HeartbeatInterval = 30 * time.Second

type SystemInfo struct {
	OS            string  `json:"os"`
	Arch          string  `json:"arch"`
	CPUCount      int     `json:"cpu_count"`
	MemoryTotalMB uint64  `json:"memory_total_mb"`
	DiskTotalGB   float64 `json:"disk_total_gb"`
	DiskUsedGB    float64 `json:"disk_used_gb"`
	ResticVersion string  `json:"restic_version"`
	RcloneVersion string  `json:"rclone_version"`
	AgentVersion  string  `json:"agent_version"`
}

type SystemInfoCollector func() SystemInfo

func DefaultSystemInfoCollector() SystemInfo {
	return SystemInfo{
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		CPUCount: runtime.NumCPU(),
	}
}

func RunHeartbeat(ctx context.Context, client *Client, collector SystemInfoCollector, interval time.Duration) {
	if interval <= 0 {
		interval = HeartbeatInterval
	}
	if collector == nil {
		collector = DefaultSystemInfoCollector
	}

	sendHeartbeat := func() {
		info := collector()
		payload := protocol.HeartbeatPayload{
			CPUPercent:    0,
			MemoryPercent: 0,
			DiskPercent:   diskPercent(info.DiskTotalGB, info.DiskUsedGB),
			ResticVersion: info.ResticVersion,
			RcloneVersion: info.RcloneVersion,
			AgentVersion:  info.AgentVersion,
			Uptime:        0,
		}

		msg, err := protocol.NewMessage(protocol.TypeHeartbeat, payload)
		if err != nil {
			log.Printf("build heartbeat failed: %v", err)
			return
		}
		if err := client.Send(*msg); err != nil {
			log.Printf("send heartbeat failed: %v", err)
		}
	}

	sendHeartbeat()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sendHeartbeat()
		}
	}
}

func diskPercent(totalGB, usedGB float64) float64 {
	if totalGB <= 0 || usedGB <= 0 {
		return 0
	}
	return usedGB / totalGB * 100
}
