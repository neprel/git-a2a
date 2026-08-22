package adapter

import (
	"context"
	"os"
	"testing"
)

func TestCommandHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Command(ctx, t.TempDir(), os.Args[0], "-test.run=TestCommandHonorsCanceledContext"); err == nil {
		t.Fatal("command ignored canceled context")
	}
}
