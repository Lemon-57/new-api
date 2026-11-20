package service

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModerationExcerptRemovesRecognizableSecretsBeforeTruncating(t *testing.T) {
	text := redactAuditExcerpt("Authorization: Bearer demo-sample-secret\n" + strings.Repeat("样", 300))
	assert.Equal(t, 240, utf8.RuneCountInString(text))
	assert.Contains(t, text, "[redacted]")
	assert.NotContains(t, text, "demo-sample-secret")
	assert.NotContains(t, text, "\n")
}

func TestModerationQueueBoundsPendingWorkWithoutWaiting(t *testing.T) {
	queue := newModerationQueue()
	proceed := make(chan struct{})
	t.Cleanup(func() { close(proceed) })
	job := moderationJob{instance: "audit-fixture", workers: 1, bytes: 64, run: func(ctx context.Context) {
		select {
		case <-proceed:
		case <-ctx.Done():
		}
	}}
	require.True(t, queue.enqueue(job, 1))
	require.True(t, queue.enqueue(job, 1))
	assert.False(t, queue.enqueue(job, 1))
	assert.Equal(t, ModerationQueueStatus{Active: 1, Pending: 1, Dropped: 1}, queue.status())
}
