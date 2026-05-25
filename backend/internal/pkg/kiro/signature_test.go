package kiro

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestThinkingSignatureGeneratedShape(t *testing.T) {
	sig := thinkingSignature("I should reason briefly.", "claude-opus-4-7", "msg_01shape")
	require.NotEmpty(t, sig)
	require.True(t, strings.HasPrefix(sig, "EqQBCgIYAhIM"), sig)
	require.GreaterOrEqual(t, len(sig), 172)

	decoded, err := base64.StdEncoding.DecodeString(sig)
	require.NoError(t, err)
	require.Len(t, decoded, 167)
	require.Equal(t, []byte{0x12, 0xa4, 0x01}, decoded[:3])
}

func TestThinkingSignatureCacheKeyIncludesMessageID(t *testing.T) {
	content := "same thinking"
	model := "claude-opus-4-7"

	sigA1 := thinkingSignature(content, model, "msg_01a")
	sigA2 := thinkingSignature(content, model, "msg_01a")
	sigB := thinkingSignature(content, model, "msg_01b")

	require.Equal(t, sigA1, sigA2)
	require.NotEqual(t, sigA1, sigB)
}
