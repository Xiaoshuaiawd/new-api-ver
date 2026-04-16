package common

import "testing"

func TestResolveRedisPoolSize(t *testing.T) {
	oldEnabled := ChatLogEnabled
	oldWorkers := ChatLogConsumerWorkers
	defer func() {
		ChatLogEnabled = oldEnabled
		ChatLogConsumerWorkers = oldWorkers
	}()

	ChatLogEnabled = false
	ChatLogConsumerWorkers = 16
	if got := resolveRedisPoolSize(10); got != 10 {
		t.Fatalf("chat log disabled should keep configured pool, got=%d", got)
	}

	ChatLogEnabled = true
	ChatLogConsumerWorkers = 16
	if got := resolveRedisPoolSize(10); got != 64 {
		t.Fatalf("expected pool auto bump to 64, got=%d", got)
	}

	ChatLogEnabled = true
	ChatLogConsumerWorkers = 80
	if got := resolveRedisPoolSize(20); got != 112 {
		t.Fatalf("expected pool auto bump to workers+32=112, got=%d", got)
	}

	ChatLogEnabled = true
	ChatLogConsumerWorkers = 16
	if got := resolveRedisPoolSize(200); got != 200 {
		t.Fatalf("configured pool larger than recommended should stay unchanged, got=%d", got)
	}
}
