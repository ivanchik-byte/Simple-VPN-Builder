package packaging_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func getProjectRoot(t *testing.T) string {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	// If in tests/packaging, navigate up two levels
	if strings.HasSuffix(wd, "tests/packaging") {
		return filepath.Dir(filepath.Dir(wd))
	}
	return wd
}

func TestGoReleaserConfig(t *testing.T) {
	root := getProjectRoot(t)
	cfgPath := filepath.Join(root, ".goreleaser.yaml")

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read .goreleaser.yaml: %v", err)
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse .goreleaser.yaml: %v", err)
	}

	ver, ok := parsed["version"].(int)
	if !ok || ver != 2 {
		t.Errorf("expected version 2 in .goreleaser.yaml, got %v", parsed["version"])
	}

	builds, ok := parsed["builds"].([]interface{})
	if !ok || len(builds) < 2 {
		t.Fatalf("expected at least 2 builds (cp and agent), got %d", len(builds))
	}

	foundCP, foundAgent := false, false
	for _, b := range builds {
		bm, ok := b.(map[string]interface{})
		if !ok {
			continue
		}
		id := bm["id"]
		if id == "vpnbuilder-cp" {
			foundCP = true
		}
		if id == "vpnbuilder-agent" {
			foundAgent = true
		}
	}

	if !foundCP {
		t.Errorf("missing vpnbuilder-cp build configuration")
	}
	if !foundAgent {
		t.Errorf("missing vpnbuilder-agent build configuration")
	}

	if _, ok := parsed["nfpms"]; !ok {
		t.Errorf("missing nfpms block for .deb/.rpm packaging")
	}
	if _, ok := parsed["checksum"]; !ok {
		t.Errorf("missing checksum configuration")
	}
	if _, ok := parsed["sboms"]; !ok {
		t.Errorf("missing sboms configuration")
	}
}

func TestSystemdUnits(t *testing.T) {
	root := getProjectRoot(t)

	units := []struct {
		file          string
		mustContain   []string
		mustNotContain []string
	}{
		{
			file: filepath.Join(root, "packaging/systemd/vpnbuilder-cp.service"),
			mustContain: []string{
				"[Unit]",
				"[Service]",
				"[Install]",
				"User=vpnbuilder",
				"ProtectSystem=strict",
				"NoNewPrivileges=true",
				"ExecStart=/usr/local/bin/vpnbuilder-cp",
			},
		},
		{
			file: filepath.Join(root, "packaging/systemd/vpnbuilder-agent.service"),
			mustContain: []string{
				"[Unit]",
				"[Service]",
				"[Install]",
				"CAP_NET_ADMIN",
				"CAP_NET_RAW",
				"LimitNOFILE=65535",
				"ProtectSystem=full",
				"ExecStart=/usr/local/bin/vpnbuilder-agent",
			},
		},
	}

	for _, u := range units {
		data, err := os.ReadFile(u.file)
		if err != nil {
			t.Fatalf("failed reading systemd unit %s: %v", u.file, err)
		}
		content := string(data)
		for _, s := range u.mustContain {
			if !strings.Contains(content, s) {
				t.Errorf("file %s missing required directive: %s", filepath.Base(u.file), s)
			}
		}
	}
}

func TestInstallScript(t *testing.T) {
	root := getProjectRoot(t)
	scriptPath := filepath.Join(root, "scripts/install.sh")

	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("install.sh not found: %v", err)
	}

	// Check executable bit
	if (info.Mode() & 0111) == 0 {
		t.Errorf("install.sh is not executable")
	}

	// Execute install.sh --help
	cmd := exec.Command("bash", scriptPath, "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh --help failed: %v, output: %s", err, string(out))
	}

	outStr := string(out)
	for _, flag := range []string{"--cp", "--agent", "--all", "--doctor", "--uninstall", "--dry-run"} {
		if !strings.Contains(outStr, flag) {
			t.Errorf("install.sh --help output missing flag %s", flag)
		}
	}
}

func TestCloudInitAndHelm(t *testing.T) {
	root := getProjectRoot(t)

	// Cloud-init
	ciPath := filepath.Join(root, "deploy/cloud-init/agent-cloud-init.yaml")
	ciData, err := os.ReadFile(ciPath)
	if err != nil {
		t.Fatalf("failed reading cloud-init template: %v", err)
	}
	var ciParsed map[string]interface{}
	if err := yaml.Unmarshal(ciData, &ciParsed); err != nil {
		t.Errorf("cloud-init template is not valid YAML: %v", err)
	}

	// Helm Chart
	chartPath := filepath.Join(root, "deploy/helm/vpnbuilder/Chart.yaml")
	chartData, err := os.ReadFile(chartPath)
	if err != nil {
		t.Fatalf("failed reading Chart.yaml: %v", err)
	}
	var chartParsed map[string]interface{}
	if err := yaml.Unmarshal(chartData, &chartParsed); err != nil {
		t.Errorf("Chart.yaml is not valid YAML: %v", err)
	}

	// Helm values
	valsPath := filepath.Join(root, "deploy/helm/vpnbuilder/values.yaml")
	valsData, err := os.ReadFile(valsPath)
	if err != nil {
		t.Fatalf("failed reading values.yaml: %v", err)
	}
	var valsParsed map[string]interface{}
	if err := yaml.Unmarshal(valsData, &valsParsed); err != nil {
		t.Errorf("values.yaml is not valid YAML: %v", err)
	}
}

func TestCommunityAndDocumentationFiles(t *testing.T) {
	root := getProjectRoot(t)

	requiredFiles := []string{
		"LICENSE",
		"CONTRIBUTING.md",
		"SECURITY.md",
		"CODE_OF_CONDUCT.md",
		"docs/ARCHITECTURE.md",
		"docs/DEPLOYMENT_GUIDE.md",
		"docs/MIGRATION_GUIDE.md",
		"docs/DEVELOPMENT.md",
		"docs/API_REFERENCE.md",
		".github/ISSUE_TEMPLATE/bug_report.yml",
		".github/ISSUE_TEMPLATE/feature_request.yml",
		".github/ISSUE_TEMPLATE/config.yml",
		".github/PULL_REQUEST_TEMPLATE.md",
		".github/dependabot.yml",
		".github/workflows/release.yml",
		".pre-commit-config.yaml",
	}

	for _, rel := range requiredFiles {
		fullPath := filepath.Join(root, rel)
		stat, err := os.Stat(fullPath)
		if err != nil {
			t.Errorf("missing required file: %s", rel)
			continue
		}
		if stat.Size() == 0 {
			t.Errorf("required file %s is empty", rel)
		}
	}
}

func TestStrictZeroEmojiEnforcement(t *testing.T) {
	root := getProjectRoot(t)

	filesToCheck := []string{
		".goreleaser.yaml",
		".github/workflows/release.yml",
		".github/ISSUE_TEMPLATE/bug_report.yml",
		".github/ISSUE_TEMPLATE/feature_request.yml",
		".github/ISSUE_TEMPLATE/config.yml",
		".github/PULL_REQUEST_TEMPLATE.md",
		".github/dependabot.yml",
		".pre-commit-config.yaml",
		"LICENSE",
		"CONTRIBUTING.md",
		"SECURITY.md",
		"CODE_OF_CONDUCT.md",
		"README.md",
		"PHASES.md",
		"scripts/install.sh",
		"packaging/systemd/vpnbuilder-cp.service",
		"packaging/systemd/vpnbuilder-agent.service",
		"docs/ARCHITECTURE.md",
		"docs/DEPLOYMENT_GUIDE.md",
		"docs/MIGRATION_GUIDE.md",
		"docs/DEVELOPMENT.md",
		"docs/API_REFERENCE.md",
		"internal/agent/doctor/doctor.go",
	}

	for _, rel := range filesToCheck {
		fullPath := filepath.Join(root, rel)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		str := string(data)
		for idx, r := range str {
			if isEmoji(r) {
				t.Errorf("emoji detected in %s at character index %d: %q (U+%04X)", rel, idx, r, r)
				break
			}
		}
	}
}

func isEmoji(r rune) bool {
	// Standard ASCII and Box Drawing / Arrows are NOT emojis
	if r >= 0x2500 && r <= 0x259F { // Box Drawing & Block Elements
		return false
	}
	if r >= 0x2190 && r <= 0x21FF { // Arrows
		return false
	}

	// Standard emoji unicode code-point blocks
	if r >= 0x1F600 && r <= 0x1F64F { // Emoticons
		return true
	}
	if r >= 0x1F300 && r <= 0x1F5FF { // Misc Symbols and Pictographs
		return true
	}
	if r >= 0x1F680 && r <= 0x1F6FF { // Transport and Map
		return true
	}
	if r >= 0x1F700 && r <= 0x1F77F { // Alchemical Symbols
		return true
	}
	if r >= 0x1F780 && r <= 0x1F7FF { // Geometric Shapes Extended
		return true
	}
	if r >= 0x1F800 && r <= 0x1F8FF { // Supplemental Arrows-C
		return true
	}
	if r >= 0x1F900 && r <= 0x1F9FF { // Supplemental Symbols and Pictographs
		return true
	}
	if r >= 0x1FA00 && r <= 0x1FA6F { // Chess Symbols
		return true
	}
	if r >= 0x1FA70 && r <= 0x1FAFF { // Symbols and Pictographs Extended-A
		return true
	}
	if r >= 0x2600 && r <= 0x26FF { // Misc symbols (e.g. check marks, sparkles)
		return true
	}
	if r >= 0x2700 && r <= 0x27BF { // Dingbats
		return true
	}
	// Emoji modifiers and components
	if r >= 0x1F1E6 && r <= 0x1F1FF { // Regional indicators (flags)
		return true
	}
	return false
}
