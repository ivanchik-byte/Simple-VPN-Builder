package manager

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AmneziaWGParams defines the obfuscation parameters used to defeat Deep Packet Inspection.
type AmneziaWGParams struct {
	Jc   int32  `json:"awg_jc,omitempty"`
	Jmin int32  `json:"awg_jmin,omitempty"`
	Jmax int32  `json:"awg_jmax,omitempty"`
	S1   int32  `json:"awg_s1,omitempty"`
	S2   int32  `json:"awg_s2,omitempty"`
	H1   uint32 `json:"awg_h1,omitempty"`
	H2   uint32 `json:"awg_h2,omitempty"`
	H3   uint32 `json:"awg_h3,omitempty"`
	H4   uint32 `json:"awg_h4,omitempty"`
}

// DefaultAmneziaWGParams provides safe standard obfuscation defaults if parameters are omitted.
var DefaultAmneziaWGParams = AmneziaWGParams{
	Jc:   4,
	Jmin: 40,
	Jmax: 70,
	S1:   64,
	S2:   64,
	H1:   1,
	H2:   2,
	H3:   3,
	H4:   4,
}

// Validate ensures parameter bounds and unique headers (MIN-02).
func (p *AmneziaWGParams) Validate() error {
	if p.Jmin > p.Jmax {
		return fmt.Errorf("Jmin (%d) must be <= Jmax (%d)", p.Jmin, p.Jmax)
	}
	if p.Jc < 0 || p.Jc > 128 {
		return fmt.Errorf("Jc (%d) out of range [0, 128]", p.Jc)
	}
	headers := map[uint32]bool{p.H1: true, p.H2: true, p.H3: true, p.H4: true}
	if len(headers) < 4 {
		return fmt.Errorf("H1, H2, H3, H4 must all be unique non-colliding headers")
	}
	return nil
}

// AmneziaWGServerConfig holds full server-side interface configuration for awg-quick or amneziawg-go.
type AmneziaWGServerConfig struct {
	Interface  string
	PrivateKey string
	ListenPort int
	AddressV4  string
	AddressV6  string
	MTU        int
	Params     AmneziaWGParams
}

// GenerateConfigFile renders an INI-formatted awg0.conf file.
func (c *AmneziaWGServerConfig) GenerateConfigFile() string {
	var b strings.Builder

	b.WriteString("[Interface]\n")
	if c.AddressV4 != "" && c.AddressV6 != "" {
		b.WriteString(fmt.Sprintf("Address = %s, %s\n", c.AddressV4, c.AddressV6))
	} else if c.AddressV4 != "" {
		b.WriteString(fmt.Sprintf("Address = %s\n", c.AddressV4))
	}

	if c.ListenPort > 0 {
		b.WriteString(fmt.Sprintf("ListenPort = %d\n", c.ListenPort))
	}
	b.WriteString(fmt.Sprintf("PrivateKey = %s\n", c.PrivateKey))
	if c.MTU > 0 {
		b.WriteString(fmt.Sprintf("MTU = %d\n", c.MTU))
	}

	// Obfuscation parameters
	p := c.Params
	if p.Jc > 0 {
		b.WriteString(fmt.Sprintf("Jc = %d\n", p.Jc))
	}
	if p.Jmin > 0 {
		b.WriteString(fmt.Sprintf("Jmin = %d\n", p.Jmin))
	}
	if p.Jmax > 0 {
		b.WriteString(fmt.Sprintf("Jmax = %d\n", p.Jmax))
	}
	if p.S1 > 0 {
		b.WriteString(fmt.Sprintf("S1 = %d\n", p.S1))
	}
	if p.S2 > 0 {
		b.WriteString(fmt.Sprintf("S2 = %d\n", p.S2))
	}
	if p.H1 > 0 {
		b.WriteString(fmt.Sprintf("H1 = %d\n", p.H1))
	}
	if p.H2 > 0 {
		b.WriteString(fmt.Sprintf("H2 = %d\n", p.H2))
	}
	if p.H3 > 0 {
		b.WriteString(fmt.Sprintf("H3 = %d\n", p.H3))
	}
	if p.H4 > 0 {
		b.WriteString(fmt.Sprintf("H4 = %d\n", p.H4))
	}

	return b.String()
}

// ParseAmneziaWGPayload parses JSON-serialized credential payload into AmneziaWGParams.
func ParseAmneziaWGPayload(data []byte) (*AmneziaWGParams, error) {
	params := DefaultAmneziaWGParams
	if len(data) == 0 {
		return &params, nil
	}

	if err := json.Unmarshal(data, &params); err != nil {
		return nil, fmt.Errorf("unmarshal amneziawg payload: %w", err)
	}
	if err := params.Validate(); err != nil {
		return nil, fmt.Errorf("invalid amneziawg parameters: %w", err)
	}
	return &params, nil
}
