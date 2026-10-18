package link

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"sync"
)

func Advertise(ctx context.Context, instance string, port int, ips []string, txt map[string]string) (stop func(), err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if instance == "" || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid bonjour instance or port")
	}
	records := make([]string, 0, len(txt))
	for key, value := range txt {
		records = append(records, key+"="+value)
	}
	sort.Strings(records)
	if len(ips) == 0 {
		return nil, fmt.Errorf("bonjour requires at least one IP")
	}
	ctx, cancel := context.WithCancel(ctx)
	args := append([]string{"-R", instance, "_polymux._tcp", "local.", strconv.Itoa(port)}, records...)
	cmd := exec.CommandContext(ctx, "/usr/bin/dns-sd", args...)
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("advertise bonjour: %w", err)
	}
	done := make(chan struct{})
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			_ = cmd.Wait()
			close(done)
		})
	}
	go func() {
		select {
		case <-ctx.Done():
			stop()
		case <-done:
		}
	}()
	return stop, nil
}
