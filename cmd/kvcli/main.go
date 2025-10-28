package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/RashikAnsar/raftkv/pkg/client"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	// colorPurple = "\033[35m"
	colorCyan  = "\033[36m"
	colorWhite = "\033[37m"
)

var (
	serverAddr = flag.String("server", "http://localhost:8080", "Server address")
	timeout    = flag.Duration("timeout", 10*time.Second, "Request timeout")
	jsonOutput = flag.Bool("json", false, "Output in JSON format")
	noColor    = flag.Bool("no-color", false, "Disable colored output")
)

func main() {
	flag.Parse()

	if flag.NArg() < 1 {
		printUsage()
		os.Exit(1)
	}

	command := flag.Arg(0)

	c := client.NewClient(client.Config{
		BaseURL: *serverAddr,
		Timeout: *timeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	var err error
	switch command {
	case "get":
		err = cmdGet(ctx, c)
	case "put":
		err = cmdPut(ctx, c)
	case "delete":
		err = cmdDelete(ctx, c)
	case "list":
		err = cmdList(ctx, c)
	case "stats":
		err = cmdStats(ctx, c)
	case "health":
		err = cmdHealth(ctx, c)
	case "ready":
		err = cmdReady(ctx, c)
	case "snapshot":
		err = cmdSnapshot(ctx, c)
	default:
		printError(fmt.Sprintf("Unknown command: %s", command))
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		printError(err.Error())
		os.Exit(1)
	}
}

func cmdGet(ctx context.Context, c *client.Client) error {
	if flag.NArg() < 2 {
		return fmt.Errorf("usage: kvcli get <key>")
	}

	key := flag.Arg(1)

	value, err := c.Get(ctx, key)
	if err != nil {
		return err
	}

	if *jsonOutput {
		output := map[string]interface{}{
			"key":   key,
			"value": string(value),
			"size":  len(value),
		}
		return printJSON(output)
	}

	fmt.Println(string(value))
	return nil
}

func cmdPut(ctx context.Context, c *client.Client) error {
	if flag.NArg() < 3 {
		return fmt.Errorf("usage: kvcli put <key> <value>")
	}

	key := flag.Arg(1)
	value := flag.Arg(2)

	if err := c.Put(ctx, key, []byte(value)); err != nil {
		return err
	}

	if *jsonOutput {
		output := map[string]interface{}{
			"status": "success",
			"key":    key,
			"size":   len(value),
		}
		return printJSON(output)
	}

	printSuccess(fmt.Sprintf("✓ Put key '%s' (%d bytes)", key, len(value)))
	return nil
}

func cmdDelete(ctx context.Context, c *client.Client) error {
	if flag.NArg() < 2 {
		return fmt.Errorf("usage: kvcli delete <key>")
	}

	key := flag.Arg(1)

	if err := c.Delete(ctx, key); err != nil {
		return err
	}

	if *jsonOutput {
		output := map[string]interface{}{
			"status": "success",
			"key":    key,
		}
		return printJSON(output)
	}

	printSuccess(fmt.Sprintf("✓ Deleted key '%s'", key))
	return nil
}

func cmdList(ctx context.Context, c *client.Client) error {
	// Parse optional flags
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	prefix := fs.String("prefix", "", "Key prefix filter")
	limit := fs.Int("limit", 0, "Maximum number of keys")
	fs.Parse(flag.Args()[1:])

	resp, err := c.List(ctx, *prefix, *limit)
	if err != nil {
		return err
	}

	if *jsonOutput {
		return printJSON(resp)
	}

	if len(resp.Keys) == 0 {
		printInfo("No keys found")
		return nil
	}

	printInfo(fmt.Sprintf("Found %d keys:", resp.Count))
	for _, key := range resp.Keys {
		fmt.Printf("  %s\n", colorize(colorCyan, key))
	}

	return nil
}

func cmdStats(ctx context.Context, c *client.Client) error {
	stats, err := c.Stats(ctx)
	if err != nil {
		return err
	}

	if *jsonOutput {
		return printJSON(stats)
	}

	printHeader("Server Statistics")
	fmt.Printf("  Gets:      %s\n", colorize(colorGreen, fmt.Sprintf("%d", stats.Gets)))
	fmt.Printf("  Puts:      %s\n", colorize(colorYellow, fmt.Sprintf("%d", stats.Puts)))
	fmt.Printf("  Deletes:   %s\n", colorize(colorRed, fmt.Sprintf("%d", stats.Deletes)))
	fmt.Printf("  Key Count: %s\n", colorize(colorBlue, fmt.Sprintf("%d", stats.KeyCount)))

	return nil
}

func cmdHealth(ctx context.Context, c *client.Client) error {
	health, err := c.Health(ctx)
	if err != nil {
		return err
	}

	if *jsonOutput {
		return printJSON(health)
	}

	if health.Status == "healthy" {
		printSuccess("✓ Server is healthy")
	} else {
		printWarning(fmt.Sprintf("⚠ Server status: %s", health.Status))
	}

	return nil
}

func cmdReady(ctx context.Context, c *client.Client) error {
	ready, err := c.Ready(ctx)
	if err != nil {
		return err
	}

	if *jsonOutput {
		return printJSON(ready)
	}

	if ready.Status == "ready" {
		printSuccess("✓ Server is ready")
	} else {
		printWarning(fmt.Sprintf("⚠ Server status: %s", ready.Status))
	}

	return nil
}

func cmdSnapshot(ctx context.Context, c *client.Client) error {
	snap, err := c.Snapshot(ctx)
	if err != nil {
		return err
	}

	if *jsonOutput {
		return printJSON(snap)
	}

	printSuccess(fmt.Sprintf("✓ Snapshot created: %s", snap.Snapshot))
	if snap.Message != "" {
		printInfo(snap.Message)
	}

	return nil
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `%s
RaftKV Command Line Interface

USAGE:
    kvcli [options] <command> [arguments]

COMMANDS:
    get <key>           Get value by key
    put <key> <value>   Put key-value pair
    delete <key>        Delete key
    list [--prefix=] [--limit=] List keys
    stats               Show server statistics
    health              Check server health
    ready               Check server readiness
    snapshot            Trigger manual snapshot

OPTIONS:
    --server=<addr>     Server address (default: http://localhost:8080)
    --timeout=<dur>     Request timeout (default: 10s)
    --json              Output in JSON format
    --no-color          Disable colored output

EXAMPLES:
    kvcli put user:1 "Alice"
    kvcli get user:1
    kvcli list --prefix=user:
    kvcli delete user:1
    kvcli stats
    kvcli snapshot

ENVIRONMENT VARIABLES:
    RAFTKV_SERVER       Server address (overrides --server)
    RAFTKV_TIMEOUT      Request timeout (overrides --timeout)

%s`, colorize(colorBlue, "RaftKV CLI v1.0.0"), colorReset)
}

func printJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func colorize(color, text string) string {
	if *noColor {
		return text
	}
	return color + text + colorReset
}

func printSuccess(msg string) {
	fmt.Println(colorize(colorGreen, msg))
}

func printError(msg string) {
	fmt.Fprintf(os.Stderr, "%s\n", colorize(colorRed, "Error: "+msg))
}

func printWarning(msg string) {
	fmt.Println(colorize(colorYellow, msg))
}

func printInfo(msg string) {
	fmt.Println(colorize(colorWhite, msg))
}

func printHeader(msg string) {
	fmt.Println(colorize(colorBlue, "=== "+msg+" ==="))
}

func init() {
	if addr := os.Getenv("RAFTKV_SERVER"); addr != "" {
		*serverAddr = addr
	}
	if t := os.Getenv("RAFTKV_TIMEOUT"); t != "" {
		if duration, err := time.ParseDuration(t); err == nil {
			*timeout = duration
		}
	}
}
