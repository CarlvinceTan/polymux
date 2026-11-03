// polymux-hub is the service half of the Hub plugin. The Polymux server starts it,
// and it runs the Matrix homeserver and bridges that Hub's chats come from.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/hub"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	config := hub.Config{Port: os.Getenv("POLYMUX_PLUGIN_PORT"), Token: os.Getenv("POLYMUX_PLUGIN_TOKEN"),
		Data: os.Getenv("POLYMUX_PLUGIN_DATA"), Root: os.Getenv("POLYMUX_PLUGIN_ROOT"),
		Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	if err := hub.Run(ctx, config); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
