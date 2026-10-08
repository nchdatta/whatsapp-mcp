package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/nchdatta/whatsapp-mcp/internal/config"
	"github.com/nchdatta/whatsapp-mcp/internal/desktop"
	"github.com/nchdatta/whatsapp-mcp/internal/update"
	"github.com/nchdatta/whatsapp-mcp/internal/wa"
)

// doctor checks the whole setup and says how to fix what's wrong.
func doctor(ctx context.Context, dataFlag string) error {
	problems := 0
	ok := func(format string, a ...any) { fmt.Printf("  OK    "+format+"\n", a...) }
	info := func(format string, a ...any) { fmt.Printf("  --    "+format+"\n", a...) }
	bad := func(fix, format string, a ...any) {
		problems++
		fmt.Printf("  FIX   "+format+"\n        -> %s\n", append(a, fix)...)
	}

	fmt.Printf("whatsapp-mcp %s\n\n", version)

	// Version
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	latest, err := update.Latest(cctx)
	cancel()
	switch {
	case err != nil:
		info("Couldn't check for updates (%v)", err)
	case update.Newer(version, latest):
		bad("run: whatsapp-mcp update", "A newer version is available: %s", latest)
	default:
		ok("Up to date (latest release %s)", latest)
	}

	// Program and Claude Desktop config
	bin, _ := desktop.BinaryPath()
	if _, err := os.Stat(bin); err == nil {
		ok("Program installed: %s", bin)
	} else {
		info("No program at %s (fine if you installed with --here)", bin)
	}
	cfgPath, err := desktop.ConfigPath()
	if err != nil {
		return err
	}
	srv, err := desktop.Current(cfgPath)
	switch {
	case err != nil:
		bad("fix the file, or delete it and run: whatsapp-mcp install", "%v", err)
	case srv == nil:
		bad("run: whatsapp-mcp install", "Not added to Claude Desktop (%s)", cfgPath)
	default:
		if _, err := os.Stat(srv.Command); err != nil {
			bad("run: whatsapp-mcp install", "Claude Desktop points at a program that doesn't exist: %s", srv.Command)
		} else {
			ok("Added to Claude Desktop: %s", cfgPath)
		}
	}
	if desktop.AppSupported() {
		if desktop.AppRunning() {
			ok("Claude Desktop is running")
		} else {
			info("Claude Desktop isn't running")
		}
	}

	// WhatsApp account
	dir, err := config.DataDir(dataFlag)
	if err != nil {
		return err
	}
	linked := false
	err = withService(dataFlag, zerolog.Disabled, func(svc *wa.Service) error {
		linked = svc.LoggedIn()
		if linked {
			ok("WhatsApp linked: +%s", svc.Client.Store.ID.User)
		} else {
			bad(`run: whatsapp-mcp login, or ask Claude "link my WhatsApp"`, "No WhatsApp account linked")
		}
		return nil
	})
	if err != nil {
		bad("see the error; your data is in "+dir, "Couldn't open the local data: %v", err)
	}

	// Recent log, which only matters for a linked account
	if linked {
		connected, errs := scanLog(filepath.Join(dir, "whatsapp-mcp.log"), 24*time.Hour)
		if !connected.IsZero() {
			ok("Last connected to WhatsApp: %s", connected.Local().Format("2006-01-02 15:04"))
		}
		for _, e := range errs {
			info("Recent problem: %s", e)
		}
	}

	// Optional tools
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		ok("ffmpeg found (any audio can be sent as a voice note)")
	} else {
		info("ffmpeg not found (optional: only .ogg/.opus files can be sent as voice notes)")
	}

	fmt.Println()
	if problems == 0 {
		fmt.Println("Everything looks fine. If WhatsApp doesn't show in Claude Desktop, restart it completely.")
	} else {
		fmt.Printf("%d thing(s) to fix; see -> above.\n", problems)
	}
	return nil
}

// scanLog reads the end of the log file and returns the last successful
// connection and up to 3 distinct warnings or errors from the last window.
func scanLog(path string, window time.Duration) (time.Time, []string) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, nil
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > 256<<10 {
		f.Seek(-256<<10, io.SeekEnd)
	}

	var connected time.Time
	var errs []string
	since := time.Now().Add(-window)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var line struct {
			Level   string    `json:"level"`
			Time    time.Time `json:"time"`
			Message string    `json:"message"`
			Error   string    `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		if line.Message == "Connected to WhatsApp" {
			connected, errs = line.Time, nil // problems before a good connection are over
			continue
		}
		// Closing the websocket on shutdown often logs this; it's harmless
		if strings.Contains(line.Message, "close to websocket") {
			continue
		}
		if (line.Level == "error" || line.Level == "warn") && line.Time.After(since) {
			msg := line.Message
			if line.Error != "" {
				msg += ": " + line.Error
			}
			if !contains(errs, msg) {
				errs = append(errs, msg)
			}
		}
	}
	if len(errs) > 3 {
		errs = errs[len(errs)-3:]
	}
	return connected, errs
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}
