package fetch

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestRetryAfterBounds(t *testing.T) {
	now := time.Now().UTC()
	for _, tt := range []struct {
		raw  string
		want time.Duration
	}{{"2", 2 * time.Second}, {"999999999", 5 * time.Second}, {"-1", 0}, {"nonsense", 0}, {now.Add(3 * time.Second).Format(http.TimeFormat), 3 * time.Second}} {
		got := retryAfter(tt.raw, now)
		if tt.raw == now.Add(3*time.Second).Format(http.TimeFormat) {
			if got < 2*time.Second || got > 3*time.Second {
				t.Fatalf("date retry: %v", got)
			}
		} else if got != tt.want {
			t.Fatalf("%q: %v", tt.raw, got)
		}
	}
}
func TestRetryAfterHonorsTotalDeadline(t *testing.T) {
	calls := 0
	c, s := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(429)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := c.Fetch(ctx, SourceRequest{URL: s.URL}); err == nil {
		t.Fatal("rate-limit accepted")
	}
	if calls != 1 || time.Since(started) > time.Second {
		t.Fatalf("retry ignored deadline: %d", calls)
	}
}
