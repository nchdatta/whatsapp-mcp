// Command whatsapp-mcp connects Claude (or any MCP client) to a
// WhatsApp account.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"

	"github.com/nchdatta/whatsapp-mcp/internal/config"
	"github.com/nchdatta/whatsapp-mcp/internal/desktop"
	"github.com/nchdatta/whatsapp-mcp/internal/mcpserver"
	"github.com/nchdatta/whatsapp-mcp/internal/wa"
)

// version is overridden at build time with -ldflags "-X main.version=..."
var version = "dev"

const usage = `whatsapp-mcp - WhatsApp for Claude and other MCP clients

Commands:
  install [--here]         add whatsapp-mcp to Claude Desktop (then restart Claude Desktop)
  uninstall                remove it from Claude Desktop
  login [--phone NUMBER]   link your WhatsApp account from a terminal (or ask Claude to link it)
  serve [--http [ADDR]]    run the MCP server on stdio (your MCP client runs this),
                           or over HTTP for remote clients such as claude.ai
  away on ["message"]      reply with a fixed message when nobody answers within --delay
                           seconds (60); also --cooldown 3h, --groups. away off / away status
  token [--rotate]       show (or replace) the secret that protects serve --http
  unlink [--delete-data]   unlink WhatsApp from this computer (alias: logout)
  status                   show the linked account
  version                  print the version

Every command accepts --data DIR (or $%s).
Default data directory: %s
`

func main() {
	if len(os.Args) < 2 && launchedByDoubleClick() {
		installInteractive()
		return
	}
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, usage, config.EnvDataDir, config.DefaultDataDir())
		os.Exit(2)
	}
	cmd := os.Args[1]

	flags := flag.NewFlagSet(cmd, flag.ExitOnError)
	dataFlag := flags.String("data", "", "data directory")
	phone := flags.String("phone", "", "login: use a pairing code for this phone number instead of a QR code")
	here := flags.Bool("here", false, "install: register this binary where it is instead of copying it to the per-user programs folder")
	deleteData := flags.Bool("delete-data", false, "unlink: also delete local message history and attachments")
	yes := flags.Bool("yes", false, "unlink/uninstall: don't ask for confirmation")
	httpAddr := flags.String("http", "", "serve: listen for MCP over HTTP on this address (default 127.0.0.1:8080 when given alone) instead of stdio")
	rotate := flags.Bool("rotate", false, "token: replace the HTTP token, invalidating old URLs")
	awayDelay := flags.Int("delay", 60, "away: seconds to wait for a reply before sending the away message")
	awayCooldown := flags.Duration("cooldown", 3*time.Hour, "away: at most one away message per chat in this time")
	awayGroups := flags.Bool("groups", false, "away: also reply in groups")
	args := os.Args[2:]
	sub := ""
	if cmd == "away" && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	positional := parseInterleaved(flags, defaultHTTPAddr(args))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch cmd {
	case "serve":
		err = withService(*dataFlag, zerolog.InfoLevel, serve(ctx, *httpAddr))
	case "away":
		err = withService(*dataFlag, zerolog.ErrorLevel, func(svc *wa.Service) error {
			return away(ctx, svc, sub, strings.Join(positional, " "),
				time.Duration(*awayDelay)*time.Second, *awayCooldown, *awayGroups)
		})
	case "token":
		err = showToken(*dataFlag, *rotate)
	case "login":
		err = withService(*dataFlag, zerolog.ErrorLevel, func(svc *wa.Service) error {
			if err := svc.Link(ctx, *phone, os.Stdout); err != nil {
				return err
			}
			printSetup(svc.DataDir())
			return nil
		})
	case "unlink", "logout":
		err = unlink(ctx, *dataFlag, *deleteData, *yes)
	case "status":
		err = withService(*dataFlag, zerolog.ErrorLevel, func(svc *wa.Service) error {
			for k, v := range svc.Status() {
				fmt.Printf("%-10s %v\n", k+":", v)
			}
			return nil
		})
	case "install":
		err = install(*dataFlag, *here)
	case "uninstall":
		err = uninstall(ctx, *dataFlag, *yes)
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

func serve(ctx context.Context, httpAddr string) func(*wa.Service) error {
	return func(svc *wa.Service) error {
		if svc.LoggedIn() {
			// Connect in the background so the client gets its tool list immediately
			go func() {
				if err := svc.Client.Connect(); err != nil {
					fmt.Fprintln(os.Stderr, "connect:", err)
				}
			}()
		} else {
			fmt.Fprintln(os.Stderr, "No WhatsApp account linked yet; ask Claude to link it (link_whatsapp) or run `whatsapp-mcp login`.")
		}
		go svc.RunAway(ctx)
		if httpAddr != "" {
			token, err := config.HTTPToken(svc.DataDir(), false)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Serving MCP over HTTP on %s\n  Local URL:  http://%s/mcp/%s\n  Put it behind HTTPS (e.g. cloudflared tunnel --url http://%s) and add\n  https://<public-host>/mcp/%s as a custom connector in claude.ai.\n  Keep this URL secret: it gives full access to your WhatsApp.\n",
				httpAddr, httpAddr, token, httpAddr, token)
			return mcpserver.RunHTTP(ctx, svc, version, httpAddr, token)
		}
		err := mcpserver.Run(ctx, svc, version)
		if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) || ctx.Err() != nil {
			return nil // client went away: normal shutdown
		}
		return err
	}
}

const defaultHTTP = "127.0.0.1:8080"

// defaultHTTPAddr lets "--http" be given without an address by filling in
// defaultHTTP when no value follows it.
func defaultHTTPAddr(args []string) []string {
	out := make([]string, 0, len(args)+1)
	for i, a := range args {
		out = append(out, a)
		if (a == "--http" || a == "-http") && (i+1 == len(args) || strings.HasPrefix(args[i+1], "-")) {
			out = append(out, defaultHTTP)
		}
	}
	return out
}

// away turns the fallback away message on or off, or shows it.
func away(ctx context.Context, svc *wa.Service, sub, message string, delay, cooldown time.Duration, groups bool) error {
	a, err := svc.AwaySettings(ctx)
	if err != nil {
		return err
	}
	switch sub {
	case "on":
		a.Enabled, a.Delay, a.Cooldown, a.Groups = true, delay, cooldown, groups
		if message != "" {
			a.Message = message
		}
	case "off":
		a.Enabled = false
	case "", "status":
	default:
		return fmt.Errorf(`unknown "away %s"; use: away on ["message"] [--delay 60] [--cooldown 3h] [--groups], away off, away status`, sub)
	}
	if sub == "on" || sub == "off" {
		if err := svc.SetAway(ctx, a); err != nil {
			return err
		}
	}
	if !a.Enabled {
		fmt.Println("Away message: off")
		return nil
	}
	where := "direct chats"
	if a.Groups {
		where = "direct chats and groups"
	}
	fmt.Printf("Away message: on\n  Text:     %s\n  Sent when a message in %s gets no reply within %s,\n  at most once per chat every %s, while whatsapp-mcp is running (Claude Desktop open).\n",
		a.Message, where, a.Delay, a.Cooldown)
	return nil
}

// showToken prints the secret path for `serve --http`.
func showToken(dataFlag string, rotate bool) error {
	dir, err := config.DataDir(dataFlag)
	if err != nil {
		return err
	}
	token, err := config.HTTPToken(dir, rotate)
	if err != nil {
		return err
	}
	if rotate {
		fmt.Println("New token created; old connector URLs stop working (restart serve --http).")
	}
	fmt.Printf("Token: %s\nConnector URL: https://<public-host>/mcp/%s\nOr send it as \"Authorization: Bearer %s\" to /mcp.\n", token, token, token)
	return nil
}

func printSetup(dataDir string) {
	fmt.Printf("\nDone. Data is stored in %s\n\nAdd it to Claude Desktop with:  whatsapp-mcp install\n", dataDir)
}

// install registers this binary in Claude Desktop's config.
func install(dataFlag string, here bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if !here {
		dst, err := desktop.BinaryPath()
		if err != nil {
			return err
		}
		if exe, err = desktop.Place(dst); err != nil {
			return err
		}
		fmt.Println("Installed binary:", exe)
		if added, err := desktop.AddToUserPath(filepath.Dir(exe)); err != nil {
			fmt.Println("Couldn't add it to PATH:", err)
		} else if added {
			fmt.Println("Added to your PATH. Open a new terminal to run `whatsapp-mcp` directly.")
		}
	}
	path, err := desktop.ConfigPath()
	if err != nil {
		return err
	}
	srv := desktop.Server{Command: exe, Args: []string{"serve"}}
	if dataFlag != "" {
		dir, err := config.DataDir(dataFlag)
		if err != nil {
			return err
		}
		srv.Args = append(srv.Args, "--data", dir)
	}
	changed, err := desktop.Install(path, srv)
	if err != nil {
		return err
	}
	if changed {
		fmt.Printf("Added to Claude Desktop: %s\n(previous config saved as claude_desktop_config.json.bak)\n", path)
	} else {
		fmt.Println("Already installed in Claude Desktop:", path)
	}

	linked := false
	if isatty.IsTerminal(os.Stdin.Fd()) {
		if linked, err = offerLink(dataFlag); err != nil {
			fmt.Println("\nLinking failed:", err)
		}
	}

	fmt.Println("\nNext:")
	fmt.Println("  - Quit Claude Desktop completely (also from the system tray / menu bar) and start it again.")
	if !linked {
		fmt.Println(`  - Link WhatsApp: run "whatsapp-mcp login", or ask Claude "link my WhatsApp".`)
	}
	return nil
}

// offerLink asks whether to link WhatsApp right away and does it in the
// terminal. It reports whether an account is linked afterwards.
func offerLink(dataFlag string) (bool, error) {
	linked := false
	err := withService(dataFlag, zerolog.ErrorLevel, func(svc *wa.Service) error {
		if svc.LoggedIn() {
			fmt.Printf("\nWhatsApp is already linked (+%s).\n", svc.Client.Store.ID.User)
			linked = true
			return nil
		}
		fmt.Print("\nLink your WhatsApp now?\n  Press Enter to show a QR code, type your phone number (with country code) for a pairing code, or type n to skip: ")
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		answer = strings.TrimSpace(answer)
		if strings.EqualFold(answer, "n") || strings.EqualFold(answer, "no") {
			return nil
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		if err := svc.Link(ctx, answer, os.Stdout); err != nil {
			return err
		}
		linked = svc.LoggedIn()
		if linked {
			fmt.Println("\nWhatsApp linked.")
		}
		return nil
	})
	return linked, err
}

func uninstall(ctx context.Context, dataFlag string, yes bool) error {
	path, err := desktop.ConfigPath()
	if err != nil {
		return err
	}
	removed, err := desktop.Uninstall(path)
	if err != nil {
		return err
	}
	if removed {
		fmt.Println("Removed from Claude Desktop. Restart it to apply.")
	} else {
		fmt.Println("Not installed in Claude Desktop:", path)
	}

	if yes || confirm("Also unlink WhatsApp and delete local messages and attachments?") {
		if err := unlink(ctx, dataFlag, true, true); err != nil && !errors.Is(err, wa.ErrNotLinked) {
			return err
		}
	}
	if bin, err := desktop.BinaryPath(); err == nil {
		desktop.RemoveFromUserPath(filepath.Dir(bin))
		if _, err := os.Stat(bin); err == nil {
			fmt.Println("To remove the program itself, delete:", bin)
		}
	}
	return nil
}

// unlink removes this computer from the WhatsApp account, optionally deleting
// local history too.
func unlink(ctx context.Context, dataFlag string, deleteData, yes bool) error {
	dir, err := config.DataDir(dataFlag)
	if err != nil {
		return err
	}
	err = withService(dataFlag, zerolog.ErrorLevel, func(svc *wa.Service) error {
		if !svc.LoggedIn() {
			return wa.ErrNotLinked
		}
		account := "+" + svc.Client.Store.ID.User
		question := "Unlink WhatsApp " + account + " from this computer?"
		if deleteData {
			question = "Unlink WhatsApp " + account + " and delete local messages and attachments?"
		}
		if !yes && !confirm(question) {
			return errCancelled
		}
		remote, err := svc.Unlink(ctx)
		if err != nil {
			return err
		}
		if remote {
			fmt.Println("Unlinked", account+". This computer is no longer listed under Linked devices.")
		} else {
			fmt.Println("Removed the local session for", account+", but WhatsApp couldn't be reached.")
			fmt.Println("On your phone, remove it under WhatsApp > Settings > Linked devices if it's still listed.")
		}
		return nil
	})
	if errors.Is(err, errCancelled) {
		fmt.Println("Cancelled.")
		return nil
	}
	switch {
	case errors.Is(err, wa.ErrNotLinked):
		if !deleteData {
			return err
		}
		// Nothing to unlink, but local data may remain; still confirm before deleting
		if !yes && !confirm("No WhatsApp account is linked. Delete local messages and attachments?") {
			fmt.Println("Cancelled.")
			return nil
		}
	case err != nil:
		return err
	}
	if deleteData {
		if err := wa.DeleteLocalData(dir); err != nil {
			return fmt.Errorf("deleting local data: %w (quit Claude Desktop if it's running, then try again)", err)
		}
		fmt.Println("Deleted local messages and attachments in", dir)
	} else {
		fmt.Println("Message history is kept in", dir)
	}
	return nil
}

var errCancelled = errors.New("cancelled")

// confirm asks a yes/no question; without a terminal it answers no.
func confirm(question string) bool {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return false
	}
	fmt.Print(question + " [y/N] ")
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

// launchedByDoubleClick reports a no-argument start from Explorer: Windows
// gives the process its own console, so it is the only process attached.
func launchedByDoubleClick() bool {
	return runtime.GOOS == "windows" && consoleProcessCount() == 1
}

// installInteractive runs install for people who double-clicked the download,
// keeping the window open so they can read the result.
func installInteractive() {
	fmt.Printf("whatsapp-mcp %s - installing into Claude Desktop\n\n", version)
	if err := install("", false); err != nil {
		fmt.Println("\nInstall failed:", err)
	}
	fmt.Print("\nPress Enter to close this window.")
	bufio.NewReader(os.Stdin).ReadString('\n')
}

// parseInterleaved parses flags that may come before, between or after
// positional arguments (the flag package stops at the first positional one),
// and returns the positional arguments.
func parseInterleaved(flags *flag.FlagSet, args []string) []string {
	var positional []string
	for {
		flags.Parse(args)
		if flags.NArg() == 0 {
			return positional
		}
		positional = append(positional, flags.Arg(0))
		args = flags.Args()[1:]
	}
}
