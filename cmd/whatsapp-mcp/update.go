package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/nchdatta/whatsapp-mcp/internal/update"
)

// runUpdate installs the latest release: it downloads and verifies the new
// binary, then runs its install, which replaces this one.
func runUpdate(ctx context.Context, dataFlag string) error {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	latest, err := update.Latest(cctx)
	cancel()
	if err != nil {
		return err
	}
	if !update.Newer(version, latest) && version != "dev" {
		fmt.Printf("Already up to date (%s).\n", version)
		return nil
	}
	fmt.Printf("Updating %s to %s\n", version, latest)

	dir, err := os.MkdirTemp("", "whatsapp-mcp-update")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	bin, err := update.Download(ctx, latest, dir)
	if err != nil {
		return err
	}
	fmt.Println("Downloaded and verified", update.AssetName())

	args := []string{"install"}
	if dataFlag != "" {
		args = append(args, "--data", dataFlag)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
