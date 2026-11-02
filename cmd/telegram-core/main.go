// telegram-core supplies a headless Telegram connection over versioned stdio.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gotd/td/tg"
	"github.com/imtaqin/telegram-cli/internal/corebridge"
)

var version = "dev"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"version": version, "protocol": corebridge.Protocol, "layer": tg.Layer})
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := corebridge.Serve(ctx, os.Stdin, os.Stdout); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "telegram-core: connection or protocol error")
		os.Exit(1)
	}
}
