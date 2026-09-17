package web

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ReadHostTelemetry probes actual real Linux system telemetry from /proc and syscall.Statfs.
func ReadHostTelemetry() (cpuPercent float64, cpuModel string, ramUsed, ramTotal, diskUsed, diskTotal int64) {
	// 1. CPU Model
	if f, err := os.Open("/proc/cpuinfo"); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "model name") {
				parts := strings.Split(line, ":")
				if len(parts) > 1 {
					cpuModel = strings.TrimSpace(parts[1])
					break
				}
			}
		}
		_ = f.Close()
	}
	if cpuModel == "" {
		cpuModel = "Linux Host Processor"
	}

	// 2. RAM from /proc/meminfo
	var memTotal, memAvailable int64
	if f, err := os.Open("/proc/meminfo"); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if fields[0] == "MemTotal:" {
					if val, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
						memTotal = val * 1024
					}
				} else if fields[0] == "MemAvailable:" {
					if val, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
						memAvailable = val * 1024
					}
				}
			}
		}
		_ = f.Close()
	}
	if memTotal > 0 {
		ramTotal = memTotal
		ramUsed = memTotal - memAvailable
	}

	// 3. Disk from Statfs
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		diskTotal = int64(stat.Blocks) * int64(stat.Bsize)
		diskFree := int64(stat.Bavail) * int64(stat.Bsize)
		diskUsed = diskTotal - diskFree
	}

	// 4. CPU Utilization calculated via delta jiffies from /proc/stat
	cpuPercent = readCPUStatPercent()

	return
}

var (
	netMeterMu   sync.Mutex
	prevRxBytes  uint64
	prevTxBytes  uint64
	prevNetTime  time.Time
	smoothedRx   float64
	smoothedTx   float64
)

// ReadHostNetworkRates calculates current network rx and tx bytes/sec from /proc/net/dev with EMA smoothing.
func ReadHostNetworkRates() (rxRate, txRate int64) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	var totalRx, totalTx uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip headers
		if strings.Contains(line, "|") || strings.HasPrefix(line, "Inter-") || strings.HasPrefix(line, "face") {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		// Ignore loopback and virtual/docker/wireguard devices for WAN rate calculation
		if iface == "lo" || strings.HasPrefix(iface, "docker") || strings.HasPrefix(iface, "veth") || strings.HasPrefix(iface, "br-") {
			continue
		}

		fields := strings.Fields(parts[1])
		if len(fields) >= 9 {
			rx, _ := strconv.ParseUint(fields[0], 10, 64)
			tx, _ := strconv.ParseUint(fields[8], 10, 64)
			totalRx += rx
			totalTx += tx
		}
	}

	netMeterMu.Lock()
	defer netMeterMu.Unlock()

	now := time.Now()
	if prevNetTime.IsZero() {
		prevRxBytes = totalRx
		prevTxBytes = totalTx
		prevNetTime = now
		return 0, 0
	}

	dt := now.Sub(prevNetTime).Seconds()
	if dt <= 0.001 {
		return int64(smoothedRx), int64(smoothedTx)
	}

	var dRx, dTx uint64
	if totalRx >= prevRxBytes {
		dRx = totalRx - prevRxBytes
	}
	if totalTx >= prevTxBytes {
		dTx = totalTx - prevTxBytes
	}

	instantRx := float64(dRx) / dt
	instantTx := float64(dTx) / dt

	// Exponential moving average smoothing (alpha = 0.35)
	const alpha = 0.35
	smoothedRx = alpha*instantRx + (1.0-alpha)*smoothedRx
	smoothedTx = alpha*instantTx + (1.0-alpha)*smoothedTx

	prevRxBytes = totalRx
	prevTxBytes = totalTx
	prevNetTime = now

	return int64(smoothedRx), int64(smoothedTx)
}

func fmtFscanf(r *os.File, format string, a ...any) (int, error) {
	scanner := bufio.NewScanner(r)
	if scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 3 && len(a) >= 3 {
			if f1, err := strconv.ParseFloat(fields[0], 64); err == nil {
				*(a[0].(*float64)) = f1
			}
			if f2, err := strconv.ParseFloat(fields[1], 64); err == nil {
				*(a[1].(*float64)) = f2
			}
			if f3, err := strconv.ParseFloat(fields[2], 64); err == nil {
				*(a[2].(*float64)) = f3
			}
			return 3, nil
		}
	}
	return 0, nil
}

var (
	cpuMu       sync.Mutex
	prevIdle    uint64
	prevTotal   uint64
	smoothedCPU float64
)

func readCPUStatPercent() float64 {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return fallbackCPUPercent()
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[0] == "cpu" {
			var user, nice, system, idle, iowait, irq, softirq, steal uint64
			if len(fields) > 1 { user, _ = strconv.ParseUint(fields[1], 10, 64) }
			if len(fields) > 2 { nice, _ = strconv.ParseUint(fields[2], 10, 64) }
			if len(fields) > 3 { system, _ = strconv.ParseUint(fields[3], 10, 64) }
			if len(fields) > 4 { idle, _ = strconv.ParseUint(fields[4], 10, 64) }
			if len(fields) > 5 { iowait, _ = strconv.ParseUint(fields[5], 10, 64) }
			if len(fields) > 6 { irq, _ = strconv.ParseUint(fields[6], 10, 64) }
			if len(fields) > 7 { softirq, _ = strconv.ParseUint(fields[7], 10, 64) }
			if len(fields) > 8 { steal, _ = strconv.ParseUint(fields[8], 10, 64) }

			totalIdle := idle + iowait
			totalNonIdle := user + nice + system + irq + softirq + steal
			total := totalIdle + totalNonIdle

			cpuMu.Lock()
			defer cpuMu.Unlock()

			if prevTotal == 0 {
				prevIdle = totalIdle
				prevTotal = total
				return fallbackCPUPercent()
			}

			totalDelta := total - prevTotal
			idleDelta := totalIdle - prevIdle

			prevIdle = totalIdle
			prevTotal = total

			if totalDelta > 0 {
				pct := (float64(totalDelta-idleDelta) / float64(totalDelta)) * 100.0
				if pct < 0.0 { pct = 0.0 }
				if pct > 100.0 { pct = 100.0 }
				smoothedCPU = 0.4*pct + 0.6*smoothedCPU
				return smoothedCPU
			}
		}
	}
	return fallbackCPUPercent()
}

func fallbackCPUPercent() float64 {
	if f, err := os.Open("/proc/loadavg"); err == nil {
		var l1, l5, l15 float64
		_, _ = fmtFscanf(f, "%f %f %f", &l1, &l5, &l15)
		_ = f.Close()
		numCPU := float64(runtime.NumCPU())
		if numCPU <= 0 { numCPU = 1 }
		pct := (l1 / numCPU) * 100.0
		if pct > 100.0 { pct = 100.0 }
		return pct
	}
	return 5.0
}
