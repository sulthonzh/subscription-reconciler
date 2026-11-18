package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadWebhookTokenRequiresProductionToken(t *testing.T) {
	token, err := loadWebhookToken("", "")

	require.Error(t, err)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "WEBHOOK_AUTH_TOKEN")
}

func TestLoadWebhookTokenAllowsExplicitTestOverride(t *testing.T) {
	token, err := loadWebhookToken("", "true")

	require.NoError(t, err)
	assert.Empty(t, token)
}

func TestLoadWebhookTokenReturnsConfiguredToken(t *testing.T) {
	token, err := loadWebhookToken("operator-secret", "")

	require.NoError(t, err)
	assert.Equal(t, "operator-secret", token)
}
