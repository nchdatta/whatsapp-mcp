// Command whatsapp-mcp connects Claude (or any MCP client) to a personal
// WhatsApp account.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"

	"github.com/nchdatta/whatsapp-mcp/internal/config"
	"github.com/nchdatta/whatsapp-mcp/internal/mcpserver"
	"github.com/nchdatta/whatsapp-mcp/internal/wa"
)

// version is overridden at build time with -ldflags "-X main.version=..."
var version = "dev"

const usage = `whatsapp-mcp - WhatsApp for Claude and other MCP clients

Commands:
  login [--phone NUMBER]   link your WhatsApp account (run once, in a terminal)
  serve                    run the MCP server on stdio (your MCP client runs this)
  logout                   unlink this device and delete the session
  status                   show the linked account
  version                  print the version

Every command accepts --data DIR (or $%s).
Default data directory: %s
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, usage, config.EnvDataDir, config.DefaultDataDir())
		os.Exit(2)
	}
	cmd := os.Args[1]

	flags := flag.NewFlagSet(cmd, flag.ExitOnError)
	dataFlag := flags.String("data", "", "data directory")
	phone := flags.String("phone", "", "login: use a pairing code for this phone number instead of a QR code")
	flags.Parse(os.Args[2:])

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch cmd {
	case "serve":
		err = withService(*dataFlag, zerolog.InfoLevel, serve(ctx))
	case "login":
		err = withService(*dataFlag, zerolog.WarnLevel, func(svc *wa.Service) error {
			if err := svc.Link(ctx, *phone, os.Stdout); err != nil {
				return err
			}
			printSetup(svc.DataDir())
			return nil
		})
	case "logout":
		err = withService(*dataFlag, zerolog.WarnLevel, func(svc *wa.Service) error {
			if err := svc.Unlink(ctx); err != nil {
				return err
			}
			fmt.Println("Unlinked. Message history is kept in", svc.DataDir())
			return nil
		})
	case "status":
		err = withService(*dataFlag, zerolog.WarnLevel, func(svc *wa.Service) error {
			for k, v := range svc.Status() {
				fmt.Printf("%-10s %v\n", k+":", v)
			}
			return nil
		})
	case "version", "-v", "--version":
		fmt.Println(version)
	case "help", "-h", "--help":
		fmt.Printf(usage, config.EnvDataDir, config.DefaultDataDir())
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n"+usage, cmd, config.EnvDataDir, config.DefaultDataDir())
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// withService opens the data directory and service around fn. Logs go to the
// log file and stderr; stdout stays free for the MCP protocol.
func withService(dataFlag string, level zerolog.Level, fn func(*wa.Service) error) error {
	dir, err := config.DataDir(dataFlag)
	if err != nil {
		return err
	}
	log, logFile, err := config.Logger(dir, os.Stderr, level)
	if err != nil {
		return err
	}
	defer logFile.Close()

	svc, err := wa.Open(context.Background(), dir, log)
	if err != nil {
		return err
	}
	defer svc.Close()
	return fn(svc)
}

func serve(ctx context.Context) func(*wa.Service) error {
	return func(svc *wa.Service) error {
		if svc.LoggedIn() {
			// Connect in the background so the client gets its tool list immediately
			go func() {
				if err := svc.Client.Connect(); err != nil {
					fmt.Fprintln(os.Stderr, "connect:", err)
				}
			}()
		} else {
			fmt.Fprintln(os.Stderr, "No WhatsApp account linked; serving local history only. Run `whatsapp-mcp login`.")
		}
		err := mcpserver.Run(ctx, svc, version)
		if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) || ctx.Err() != nil {
			return nil // client went away: normal shutdown
		}
		return err
	}
}

func printSetup(dataDir string) {
	exe, _ := os.Executable()
	fmt.Printf(`
Done. Data is stored in %s

Connect it to Claude:
  Claude Code:     claude mcp add whatsapp -- "%s" serve
  Claude Desktop:  add to claude_desktop_config.json
                   "mcpServers": { "whatsapp": { "command": %q, "args": ["serve"] } }
`, dataDir, exe, exe)
}
