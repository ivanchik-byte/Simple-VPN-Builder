package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
)

// CheckStatus represents the status of an individual diagnostic check.
type CheckStatus string

const (
	StatusPass CheckStatus = "PASS"
	StatusWarn CheckStatus = "WARN"
	StatusFail CheckStatus = "FAIL"
)

// DiagnosticItem records the result of a single check.
type DiagnosticItem struct {
	Name    string      `json:"name"`
	Status  CheckStatus `json:"status"`
	Details string      `json:"details"`
	Error   string      `json:"error,omitempty"`
}

// DiagnosticReport represents the complete diagnostic summary.
type DiagnosticReport struct {
	Timestamp       string           `json:"timestamp"`
	OS              string           `json:"os"`
	Arch            string           `json:"arch"`
	KernelRelease   string           `json:"kernel_release"`
	Hostname        string           `json:"hostname"`
	Checks          []DiagnosticItem `json:"checks"`
	PassedCount     int              `json:"passed_count"`
	WarningCount    int              `json:"warning_count"`
	FailedCount     int              `json:"failed_count"`
	ReadyForRouting bool             `json:"ready_for_routing"`
}

// Doctor orchestrates diagnostic evaluations on the node.
type Doctor struct {
	cfg *config.AgentConfig
}

// NewDoctor creates a new diagnostic doctor.
func NewDoctor(cfg *config.AgentConfig) *Doctor {
	return &Doctor{cfg: cfg}
}

// Run executes all diagnostic checks and compiles a report.
func (d *Doctor) Run(ctx context.Context) DiagnosticReport {
	report := DiagnosticReport{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Checks:    make([]DiagnosticItem, 0),
	}

	hostname, _ := os.Hostname()
	report.Hostname = hostname
	report.KernelRelease = readKernelRelease()

	// 1. Operating System
	if runtime.GOOS != "linux" {
		report.addCheck(DiagnosticItem{
			Name:    "Operating System",
			Status:  StatusWarn,
			Details: fmt.Sprintf("Running on %s (production node agent requires Linux for WireGuard/nftables)", runtime.GOOS),
		})
	} else {
		report.addCheck(DiagnosticItem{
			Name:    "Operating System",
			Status:  StatusPass,
			Details: fmt.Sprintf("Linux (%s)", report.KernelRelease),
		})
	}

	// 2. Kernel Modules
	d.checkKernelModules(&report)

	// 3. Sysctl Forwarding and Congestion Control
	d.checkSysctls(&report)

	// 4. Permissions and User Privileges
	d.checkPrivileges(&report)

	// 5. Firewall Engine Availability
	d.checkFirewall(&report)

	// 6. Network Interfaces and MTU
	d.checkNetwork(&report)

	// 7. Port Availability
	d.checkPorts(&report)

	// 8. Control Plane Connectivity (if config supplied)
	if d.cfg != nil && d.cfg.ControlPlane != "" {
		d.checkControlPlane(ctx, &report)
	}

	// Calculate summary counts
	for _, c := range report.Checks {
		switch c.Status {
		case StatusPass:
			report.PassedCount++
		case StatusWarn:
			report.WarningCount++
		case StatusFail:
			report.FailedCount++
		}
	}

	report.ReadyForRouting = report.FailedCount == 0
	return report
}

func (r *DiagnosticReport) addCheck(item DiagnosticItem) {
	r.Checks = append(r.Checks, item)
}

func (d *Doctor) checkKernelModules(report *DiagnosticReport) {
	if runtime.GOOS != "linux" {
		return
	}

	modulesContent, err := os.ReadFile("/proc/modules")
	modulesStr := ""
	if err == nil {
		modulesStr = string(modulesContent)
	}

	// WireGuard
	if strings.Contains(modulesStr, "wireguard") {
		report.addCheck(DiagnosticItem{
			Name:    "Kernel Module: wireguard",
			Status:  StatusPass,
			Details: "Module is loaded in kernel",
		})
	} else {
		// Attempt modprobe verification or built-in check
		if checkModprobe("wireguard") {
			report.addCheck(DiagnosticItem{
				Name:    "Kernel Module: wireguard",
				Status:  StatusPass,
				Details: "Module available and loadable",
			})
		} else {
			report.addCheck(DiagnosticItem{
				Name:    "Kernel Module: wireguard",
				Status:  StatusWarn,
				Details: "wireguard module not currently in /proc/modules (may be built into kernel or will auto-load)",
			})
		}
	}

	// AmneziaWG
	if strings.Contains(modulesStr, "amneziawg") {
		report.addCheck(DiagnosticItem{
			Name:    "Kernel Module: amneziawg",
			Status:  StatusPass,
			Details: "AmneziaWG obfuscation kernel module loaded",
		})
	} else {
		report.addCheck(DiagnosticItem{
			Name:    "Kernel Module: amneziawg",
			Status:  StatusWarn,
			Details: "amneziawg module not detected in /proc/modules (obfuscated AWG headers will use userspace fallback)",
		})
	}
}

func (d *Doctor) checkSysctls(report *DiagnosticReport) {
	if runtime.GOOS != "linux" {
		return
	}

	// IPv4 forwarding
	ipv4Val, err := readSysctl("/proc/sys/net/ipv4/ip_forward")
	if err == nil && strings.TrimSpace(ipv4Val) == "1" {
		report.addCheck(DiagnosticItem{
			Name:    "Sysctl: net.ipv4.ip_forward",
			Status:  StatusPass,
			Details: "Enabled (1)",
		})
	} else {
		report.addCheck(DiagnosticItem{
			Name:    "Sysctl: net.ipv4.ip_forward",
			Status:  StatusFail,
			Details: fmt.Sprintf("Current value: %s (must be 1 for VPN packet forwarding)", strings.TrimSpace(ipv4Val)),
		})
	}

	// IPv6 forwarding
	ipv6Val, err := readSysctl("/proc/sys/net/ipv6/conf/all/forwarding")
	if err == nil && strings.TrimSpace(ipv6Val) == "1" {
		report.addCheck(DiagnosticItem{
			Name:    "Sysctl: net.ipv6.conf.all.forwarding",
			Status:  StatusPass,
			Details: "Enabled (1)",
		})
	} else {
		report.addCheck(DiagnosticItem{
			Name:    "Sysctl: net.ipv6.conf.all.forwarding",
			Status:  StatusWarn,
			Details: fmt.Sprintf("Current value: %s (recommended 1 if IPv6 dual-stack is used)", strings.TrimSpace(ipv6Val)),
		})
	}

	// TCP congestion control
	ccVal, err := readSysctl("/proc/sys/net/ipv4/tcp_congestion_control")
	if err == nil {
		ccTrim := strings.TrimSpace(ccVal)
		if ccTrim == "bbr" {
			report.addCheck(DiagnosticItem{
				Name:    "Sysctl: tcp_congestion_control",
				Status:  StatusPass,
				Details: "Configured to BBR (optimal throughput)",
			})
		} else {
			report.addCheck(DiagnosticItem{
				Name:    "Sysctl: tcp_congestion_control",
				Status:  StatusWarn,
				Details: fmt.Sprintf("Current: %s (BBR recommended for high-bandwidth egress)", ccTrim),
			})
		}
	}
}

func (d *Doctor) checkPrivileges(report *DiagnosticReport) {
	uid := os.Geteuid()
	if uid == 0 {
		report.addCheck(DiagnosticItem{
			Name:    "User Privileges",
			Status:  StatusPass,
			Details: "Running as root (UID 0)",
		})
		return
	}

	// Non-root check: verify ambient capabilities if on Linux
	if runtime.GOOS == "linux" {
		statusData, err := os.ReadFile("/proc/self/status")
		if err == nil && (strings.Contains(string(statusData), "CapEff") || strings.Contains(string(statusData), "CapBnd")) {
			report.addCheck(DiagnosticItem{
				Name:    "User Privileges",
				Status:  StatusWarn,
				Details: fmt.Sprintf("Running as UID %d (ensure systemd AmbientCapabilities has CAP_NET_ADMIN and CAP_NET_RAW)", uid),
			})
			return
		}
	}

	report.addCheck(DiagnosticItem{
		Name:    "User Privileges",
		Status:  StatusWarn,
		Details: fmt.Sprintf("Running as UID %d (requires CAP_NET_ADMIN / CAP_NET_RAW for wireguard and nftables)", uid),
	})
}

func (d *Doctor) checkFirewall(report *DiagnosticReport) {
	nftPath, errNft := exec.LookPath("nft")
	iptPath, errIpt := exec.LookPath("iptables")

	if errNft == nil {
		report.addCheck(DiagnosticItem{
			Name:    "Firewall Engine: nftables",
			Status:  StatusPass,
			Details: fmt.Sprintf("Found %s", nftPath),
		})
	} else if errIpt == nil {
		report.addCheck(DiagnosticItem{
			Name:    "Firewall Engine: iptables",
			Status:  StatusWarn,
			Details: fmt.Sprintf("Found legacy %s (nftables strongly recommended)", iptPath),
		})
	} else {
		report.addCheck(DiagnosticItem{
			Name:    "Firewall Engine",
			Status:  StatusFail,
			Details: "Neither nftables (nft) nor iptables was found in PATH",
		})
	}
}

func (d *Doctor) checkNetwork(report *DiagnosticReport) {
	interfaces, err := net.Interfaces()
	if err != nil {
		report.addCheck(DiagnosticItem{
			Name:    "Network Interfaces",
			Status:  StatusWarn,
			Details: "Unable to enumerate network interfaces",
			Error:   err.Error(),
		})
		return
	}

	activeCount := 0
	primaryMTU := 1500
	var primaryName string

	for _, iface := range interfaces {
		if (iface.Flags&net.FlagUp) != 0 && (iface.Flags&net.FlagLoopback) == 0 {
			activeCount++
			if primaryName == "" {
				primaryName = iface.Name
				primaryMTU = iface.MTU
			}
		}
	}

	if activeCount == 0 {
		report.addCheck(DiagnosticItem{
			Name:    "Network Interfaces",
			Status:  StatusFail,
			Details: "No active non-loopback network interfaces detected",
		})
		return
	}

	recommendedWgMTU := primaryMTU - 80
	if recommendedWgMTU > 1420 {
		recommendedWgMTU = 1420
	}

	report.addCheck(DiagnosticItem{
		Name:    "Network Interfaces",
		Status:  StatusPass,
		Details: fmt.Sprintf("%d active interface(s). Primary '%s' MTU: %d (Recommended WG MTU: %d)", activeCount, primaryName, primaryMTU, recommendedWgMTU),
	})
}

func (d *Doctor) checkPorts(report *DiagnosticReport) {
	ports := []struct {
		network string
		port    int
		proto   string
		role    string
	}{
		{"udp", 51820, "UDP", "WireGuard listen port"},
		{"tcp", 443, "TCP", "Xray VLESS-Reality TLS port"},
		{"tcp", 8081, "TCP", "Agent health/metrics port"},
	}

	for _, p := range ports {
		addr := fmt.Sprintf("0.0.0.0:%d", p.port)
		var ln io.Closer
		var err error

		if p.network == "udp" {
			var uLn *net.UDPConn
			uLn, err = net.ListenUDP("udp", &net.UDPAddr{Port: p.port})
			ln = uLn
		} else {
			ln, err = net.Listen("tcp", addr)
		}

		if err != nil {
			report.addCheck(DiagnosticItem{
				Name:    fmt.Sprintf("Port Availability: %s %d", p.proto, p.port),
				Status:  StatusWarn,
				Details: fmt.Sprintf("Port occupied or permission denied: %v (%s)", err, p.role),
			})
		} else {
			_ = ln.Close()
			report.addCheck(DiagnosticItem{
				Name:    fmt.Sprintf("Port Availability: %s %d", p.proto, p.port),
				Status:  StatusPass,
				Details: fmt.Sprintf("Available (%s)", p.role),
			})
		}
	}
}

func (d *Doctor) checkControlPlane(ctx context.Context, report *DiagnosticReport) {
	target := d.cfg.ControlPlane
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		host = target
		port = "9090"
	}

	dialer := &net.Dialer{Timeout: 3 * time.Second}
	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	duration := time.Since(start)

	if err != nil {
		report.addCheck(DiagnosticItem{
			Name:    "Control Plane Connectivity",
			Status:  StatusFail,
			Details: fmt.Sprintf("Cannot connect to %s: %v", target, err),
		})
	} else {
		_ = conn.Close()
		report.addCheck(DiagnosticItem{
			Name:    "Control Plane Connectivity",
			Status:  StatusPass,
			Details: fmt.Sprintf("Connected to %s in %s", target, duration.Round(time.Millisecond)),
		})
	}
}

// PrintHuman outputs a formatted ASCII table report to the given writer.
func (r *DiagnosticReport) PrintHuman(w io.Writer) {
	_, _ = fmt.Fprintf(w, "======================================================================\n")
	_, _ = fmt.Fprintf(w, "           Simple-VPN-Builder Agent Diagnostics (Doctor)\n")
	_, _ = fmt.Fprintf(w, "======================================================================\n")
	_, _ = fmt.Fprintf(w, "Timestamp:      %s\n", r.Timestamp)
	_, _ = fmt.Fprintf(w, "Host:           %s (%s/%s)\n", r.Hostname, r.OS, r.Arch)
	if r.KernelRelease != "" {
		_, _ = fmt.Fprintf(w, "Kernel:         %s\n", r.KernelRelease)
	}
	_, _ = fmt.Fprintf(w, "----------------------------------------------------------------------\n")

	for _, c := range r.Checks {
		tag := fmt.Sprintf("[%s]", c.Status)
		_, _ = fmt.Fprintf(w, "%-8s %-32s : %s\n", tag, c.Name, c.Details)
	}

	_, _ = fmt.Fprintf(w, "======================================================================\n")
	_, _ = fmt.Fprintf(w, "Summary: %d passed, %d warnings, %d errors.\n", r.PassedCount, r.WarningCount, r.FailedCount)
	if r.ReadyForRouting {
		_, _ = fmt.Fprintf(w, "Status:  READY FOR ROUTING\n")
	} else {
		_, _ = fmt.Fprintf(w, "Status:  ACTION REQUIRED (Fix failed checks before launching agent)\n")
	}
	_, _ = fmt.Fprintf(w, "======================================================================\n")
}

// PrintJSON outputs the report in indented JSON.
func (r *DiagnosticReport) PrintJSON(w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(r)
}

func readSysctl(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func readKernelRelease() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err == nil {
		return strings.TrimSpace(string(data))
	}
	return ""
}

func checkModprobe(mod string) bool {
	cmd := exec.Command("modprobe", "-n", mod)
	return cmd.Run() == nil
}
