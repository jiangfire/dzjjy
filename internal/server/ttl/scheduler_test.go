package ttl

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSchedulerOnlyFiresLatestGeneration(t *testing.T) {
	scheduler := New()
	defer scheduler.Close()
	var called atomic.Uint64
	fired := make(chan uint64, 2)
	callback := func(_ string, generation uint64) { called.Add(1); fired <- generation }
	scheduler.Schedule("app", 1, time.Now().Add(30*time.Millisecond), callback)
	scheduler.Schedule("app", 2, time.Now().Add(70*time.Millisecond), callback)
	select {
	case generation := <-fired:
		require.Equal(t, uint64(2), generation)
	case <-time.After(time.Second):
		t.Fatal("current generation did not expire")
	}
	require.Equal(t, uint64(1), called.Load())
}

func TestSchedulerCancelSuppressesExpiration(t *testing.T) {
	scheduler := New()
	defer scheduler.Close()
	fired := make(chan struct{}, 1)
	scheduler.Schedule("app", 1, time.Now().Add(20*time.Millisecond), func(string, uint64) { fired <- struct{}{} })
	scheduler.Cancel("app")
	select {
	case <-fired:
		t.Fatal("cancelled entry fired")
	case <-time.After(60 * time.Millisecond):
	}
}
