package setting

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestModerationKeyNormalizationAndEncryption(t *testing.T) {
	keys, err := NormalizeContentModerationKeys(" key-a\r\nkey-b\nkey-a\n\n")
	require.NoError(t, err)
	require.Equal(t, []string{"key-a", "key-b"}, keys)
	for _, invalid := range []string{"a,b", "a b", "a\tb", "key\x00x", strings.Repeat("y", 8193)} {
		_, err := NormalizeContentModerationKeys(invalid)
		require.Error(t, err)
	}
	t.Setenv("CRYPTO_SECRET", strings.Repeat("test-only", 5))
	sealed, err := EncryptContentModerationKey(strings.Join(keys, "\n"))
	require.NoError(t, err)
	require.NotContains(t, sealed, "key-a")
	plain, err := DecryptContentModerationKey(sealed)
	require.NoError(t, err)
	require.Equal(t, "key-a\nkey-b", plain)
}

func TestModerationPoolFreezesAndRecovers(t *testing.T) {
	for _, tc := range []struct {
		status int
		freeze time.Duration
	}{{0, 0}, {400, 0}, {401, 10 * time.Minute}, {403, 10 * time.Minute}, {429, time.Minute}, {529, time.Minute}, {500, 10 * time.Second}, {200, 10 * time.Second}} {
		c := ContentModerationConfig{APIKey: "synthetic-key", BaseURL: t.Name() + strconv.Itoa(tc.status), Model: "test"}
		key, ok := BeginContentModerationKey(c)
		require.True(t, ok)
		FinishContentModerationKey(c, key, tc.status, 5*time.Millisecond, errors.New("must not expose synthetic-key"))
		states := ContentModerationKeyStatuses(c)
		require.EqualValues(t, 1, states[0].Calls)
		require.EqualValues(t, 1, states[0].Errors)
		require.Zero(t, states[0].Active)
		require.NotContains(t, states[0].LastError, "synthetic-key")
		if tc.freeze > 0 {
			require.WithinDuration(t, time.Now().Add(tc.freeze), states[0].FrozenUntil, time.Second)
			_, ok = BeginContentModerationKey(c)
			require.False(t, ok)
			moderationKeyPool.Lock()
			moderationKeyState(c, key).FrozenUntil = time.Now().Add(-time.Second)
			moderationKeyPool.Unlock()
		}
		key, ok = BeginContentModerationKey(c)
		require.True(t, ok)
		FinishContentModerationKey(c, key, 200, time.Millisecond, nil)
		states = ContentModerationKeyStatuses(c)
		require.Empty(t, states[0].LastError)
		require.True(t, states[0].FrozenUntil.IsZero())
		require.EqualValues(t, 1, states[0].Successes)
	}
}

func TestModerationPoolConcurrentRotation(t *testing.T) {
	c := ContentModerationConfig{APIKey: "synthetic-rotate-a\nsynthetic-rotate-b", BaseURL: t.Name()}
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			key, ok := BeginContentModerationKey(c)
			if !ok {
				t.Error("missing usable key")
				return
			}
			FinishContentModerationKey(c, key, 200, time.Millisecond, nil)
		})
	}
	wg.Wait()
	for _, state := range ContentModerationKeyStatuses(c) {
		require.EqualValues(t, 50, state.Calls)
		require.Zero(t, state.Active)
	}
}
