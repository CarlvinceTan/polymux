package link

import (
	"context"
	"testing"
)

func TestAdvertiseEmpty(t *testing.T) {
	stop, err := Advertise(context.Background(), "test", 7339, nil, nil)
	if err == nil || stop != nil {
		t.Fatalf("Advertise without IPs: stop nil = %v, error = %v; want nil stop and error", stop == nil, err)
	}
}
