package manager

import (
	"fmt"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIPAllocator_AllocateAndRelease(t *testing.T) {
	v4Prefix := netip.MustParsePrefix("10.8.0.0/16")
	v6Prefix := netip.MustParsePrefix("fd00::/64")
	alloc := NewIPAllocator(v4Prefix, v6Prefix)

	// Allocate for peer 1
	v4, v6, err := alloc.Allocate("peer-1")
	require.NoError(t, err)
	assert.True(t, v4.IsValid())
	assert.True(t, v6.IsValid())
	assert.True(t, v4Prefix.Contains(v4))
	assert.True(t, v6Prefix.Contains(v6))

	// Calling Allocate again for same peer returns same IPs
	v4Same, v6Same, err := alloc.Allocate("peer-1")
	require.NoError(t, err)
	assert.Equal(t, v4, v4Same)
	assert.Equal(t, v6, v6Same)

	// Allocate for peer 2 must be different
	v4Other, v6Other, err := alloc.Allocate("peer-2")
	require.NoError(t, err)
	assert.NotEqual(t, v4, v4Other)
	assert.NotEqual(t, v6, v6Other)

	// Release peer 1
	alloc.Release("peer-1")

	// Peer 1 addresses are freed and can be reused
	assert.False(t, alloc.usedV4[v4])
	assert.False(t, alloc.usedV6[v6])
}

func TestIPAllocator_Exceeds256Addresses(t *testing.T) {
	v4Prefix := netip.MustParsePrefix("10.8.0.0/16")
	v6Prefix := netip.MustParsePrefix("fd00::/64")
	alloc := NewIPAllocator(v4Prefix, v6Prefix)

	allocated := make(map[string]bool)

	// Allocate 300 addresses without truncation collisions
	for i := 1; i <= 300; i++ {
		peerID := fmt.Sprintf("peer-%d", i)
		v4, _, err := alloc.Allocate(peerID)
		require.NoError(t, err, "Failed at index %d", i)

		ipStr := v4.String()
		assert.False(t, allocated[ipStr], "Duplicate IP detected at index %d: %s", i, ipStr)
		allocated[ipStr] = true
	}

	assert.Len(t, allocated, 300)
}
