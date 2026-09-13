package syncer

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

var (
	cpuSampleMu sync.Mutex
	prevIdle    uint64
	prevTotal   uint64
)

func (s *Syncer) collectSystemInfo() *agentv1.SystemInfo {
	info := &agentv1.SystemInfo{
		UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
	}

	// 1. RAM from /proc/meminfo
	if f, err := os.Open("/proc/meminfo"); err == nil {
		scanner := bufio.NewScanner(f)
		var memTotal, memAvailable uint64
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 2 {
				if fields[0] == "MemTotal:" {
					if val, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
						memTotal = val * 1024
					}
				} else if fields[0] == "MemAvailable:" {
					if val, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
						memAvailable = val * 1024
					}
				}
			}
		}
		_ = f.Close()
		if memTotal > 0 {
			info.MemoryTotal = memTotal
			if memTotal >= memAvailable {
				info.MemoryUsed = memTotal - memAvailable
			}
		}
	}

	// 2. Disk from syscall.Statfs
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		diskTotal := stat.Blocks * uint64(stat.Bsize)
		diskFree := stat.Bavail * uint64(stat.Bsize)
		info.DiskTotal = diskTotal
		if diskTotal >= diskFree {
			info.DiskUsed = diskTotal - diskFree
		}
	}

	// 3. CPU from /proc/stat
	info.CpuUsagePercent = readCPUSample()

	return info
}

func readCPUSample() float64 {
	cpuSampleMu.Lock()
	defer cpuSampleMu.Unlock()

	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0.0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return 0.0
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0.0
	}

	var total uint64
	var idle uint64
	for i := 1; i < len(fields); i++ {
		val, _ := strconv.ParseUint(fields[i], 10, 64)
		total += val
		if i == 4 {
			idle = val
		}
	}

	if prevTotal == 0 {
		prevTotal = total
		prevIdle = idle
		return 0.5
	}

	if total <= prevTotal {
		return 0.0
	}

	deltaTotal := total - prevTotal
	var deltaIdle uint64
	if idle >= prevIdle {
		deltaIdle = idle - prevIdle
	}

	prevTotal = total
	prevIdle = idle

	if deltaTotal == 0 {
		return 0.0
	}

	pct := float64(deltaTotal-deltaIdle) / float64(deltaTotal) * 100.0
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	return pct
}
