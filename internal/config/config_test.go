package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEnvInt64(t *testing.T) {
	t.Run("Unset variable returns fallback", func(t *testing.T) {
		val, err := getEnvInt64("NON_EXISTENT_VAR_12345", 42)
		require.NoError(t, err)
		assert.Equal(t, int64(42), val)
	})

	t.Run("Empty variable returns fallback", func(t *testing.T) {
		t.Setenv("TEST_EMPTY_INT_VAR", "")
		val, err := getEnvInt64("TEST_EMPTY_INT_VAR", 99)
		require.NoError(t, err)
		assert.Equal(t, int64(99), val)
	})

	t.Run("Valid positive integer returns parsed value", func(t *testing.T) {
		t.Setenv("TEST_VALID_INT_VAR", "1048576")
		val, err := getEnvInt64("TEST_VALID_INT_VAR", 100)
		require.NoError(t, err)
		assert.Equal(t, int64(1048576), val)
	})

	t.Run("Negative integer returns error", func(t *testing.T) {
		t.Setenv("TEST_NEG_INT_VAR", "-5")
		_, err := getEnvInt64("TEST_NEG_INT_VAR", 100)
		require.Error(t, err)
	})

	t.Run("Zero returns error", func(t *testing.T) {
		t.Setenv("TEST_ZERO_INT_VAR", "0")
		_, err := getEnvInt64("TEST_ZERO_INT_VAR", 100)
		require.Error(t, err)
	})

	t.Run("Non-numeric string returns error", func(t *testing.T) {
		t.Setenv("TEST_NONNUM_INT_VAR", "invalid123")
		_, err := getEnvInt64("TEST_NONNUM_INT_VAR", 100)
		require.Error(t, err)
	})
}

func TestLoad_InvalidIntegerEnvFails(t *testing.T) {
	t.Setenv("AUTH_SECRET", "very-secure-secret-key-for-development-mode")
	t.Setenv("MAX_IMAGE_SIZE", "notanumber")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MAX_IMAGE_SIZE")
}
