package web

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
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

	// 4. CPU Load from /proc/loadavg
	if f, err := os.Open("/proc/loadavg"); err == nil {
		var l1, l5, l15 float64
		_, _ = fmtFscanf(f, "%f %f %f", &l1, &l5, &l15)
		_ = f.Close()
		cpuPercent = l1 * 10.0
		if cpuPercent > 100.0 {
			cpuPercent = 100.0
		}
	}

	return
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
