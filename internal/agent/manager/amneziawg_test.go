package manager

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAmneziaWGServerConfig_GenerateConfigFile(t *testing.T) {
	cfg := &AmneziaWGServerConfig{
		Interface:  "awg0",
		PrivateKey: "privkey123=",
		ListenPort: 51821,
		AddressV4:  "10.8.1.1/24",
		MTU:        1360,
		Params: AmneziaWGParams{
			Jc:   5,
			Jmin: 50,
			Jmax: 80,
			S1:   64,
			S2:   48,
			H1:   12345678,
			H2:   23456789,
			H3:   34567890,
			H4:   45678901,
		},
	}

	conf := cfg.GenerateConfigFile()
	assert.Contains(t, conf, "[Interface]")
	assert.Contains(t, conf, "Address = 10.8.1.1/24")
	assert.Contains(t, conf, "ListenPort = 51821")
	assert.Contains(t, conf, "PrivateKey = privkey123=")
	assert.Contains(t, conf, "MTU = 1360")
	assert.Contains(t, conf, "Jc = 5")
	assert.Contains(t, conf, "Jmin = 50")
	assert.Contains(t, conf, "Jmax = 80")
	assert.Contains(t, conf, "S1 = 64")
	assert.Contains(t, conf, "S2 = 48")
	assert.Contains(t, conf, "H1 = 12345678")
	assert.Contains(t, conf, "H4 = 45678901")
}

func TestParseAmneziaWGPayload(t *testing.T) {
	data, err := json.Marshal(AmneziaWGParams{
		Jc:   3,
		Jmin: 60,
		Jmax: 120,
		H1:   999,
	})
	require.NoError(t, err)

	p, err := ParseAmneziaWGPayload(data)
	require.NoError(t, err)
	assert.Equal(t, int32(3), p.Jc)
	assert.Equal(t, int32(60), p.Jmin)
	assert.Equal(t, int32(120), p.Jmax)
	assert.Equal(t, int64(999), p.H1)
}
