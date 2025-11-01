package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
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
	serverAddr = flag.String("server", "localhost:8080", "Server address (without protocol prefix)")
	protocol   = flag.String("protocol", "http", "Protocol to use: http or grpc")
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

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	var err error

	// Use protocol-specific client
	switch *protocol {
	case "grpc":
		err = runWithGRPC(ctx, command)
	case "http":
		err = runWithHTTP(ctx, command)
	default:
		printError(fmt.Sprintf("Unknown protocol: %s (use 'http' or 'grpc')", *protocol))
		os.Exit(1)
	}

	if err != nil {
		printError(err.Error())
		os.Exit(1)
	}
}

func runWithHTTP(ctx context.Context, command string) error {
	// Add http:// prefix if not present
	baseURL := *serverAddr
	if baseURL[:4] != "http" {
		baseURL = "http://" + baseURL
	}

	c := client.NewClient(client.Config{
		BaseURL: baseURL,
		Timeout: *timeout,
	})

	switch command {
	case "get":
		return cmdGet(ctx, c)
	case "put":
		return cmdPut(ctx, c)
	case "delete":
		return cmdDelete(ctx, c)
	case "list":
		return cmdList(ctx, c)
	case "stats":
		return cmdStats(ctx, c)
	case "health":
		return cmdHealth(ctx, c)
	case "ready":
		return cmdReady(ctx, c)
	case "snapshot":
		return cmdSnapshot(ctx, c)
	default:
		printError(fmt.Sprintf("Unknown command: %s", command))
		printUsage()
		os.Exit(1)
		return nil
	}
}

func runWithGRPC(ctx context.Context, command string) error {
	// Auto-configure gRPC servers based on HTTP ports
	// Automatically converts HTTP ports (808x) to gRPC ports (909x)
	// and adds all cluster servers for auto-discovery
	grpcServers := autoConfigureGRPCServers(*serverAddr)

	grpcClient, err := client.NewGRPCClient(client.GRPCClientConfig{
		Servers:         grpcServers,
		Timeout:         *timeout,
		MaxRetries:      3,
		EnableAutoRetry: true, // Enabled - server returns gRPC addresses
	})
	if err != nil {
		return fmt.Errorf("failed to create gRPC client: %w", err)
	}
	defer grpcClient.Close()

	switch command {
	case "get":
		return cmdGetGRPC(ctx, grpcClient)
	case "put":
		return cmdPutGRPC(ctx, grpcClient)
	case "delete":
		return cmdDeleteGRPC(ctx, grpcClient)
	case "list":
		return cmdListGRPC(ctx, grpcClient)
	case "stats":
		return cmdStatsGRPC(ctx, grpcClient)
	case "health":
		printWarning("Health check not supported over gRPC, use HTTP")
		return nil
	case "ready":
		printWarning("Ready check not supported over gRPC, use HTTP")
		return nil
	case "snapshot":
		printWarning("Snapshot not supported over gRPC, use HTTP")
		return nil
	default:
		printError(fmt.Sprintf("Unknown command: %s", command))
		printUsage()
		os.Exit(1)
		return nil
	}
}

// autoConfigureGRPCServers converts HTTP server addresses to gRPC addresses
// and adds all cluster servers for better auto-discovery.
//
// Rules:
// - If user provides HTTP ports (808x), convert to gRPC ports (909x)
// - If user provides a single cluster server, auto-add all three for discovery
// - If user provides gRPC ports directly, use as-is
// - Otherwise, use addresses as-is
//
// Examples:
//   - "localhost:8081" → ["localhost:9091", "localhost:9092", "localhost:9093"]
//   - "localhost:9091" → ["localhost:9091", "localhost:9092", "localhost:9093"]
//   - "localhost:8081,localhost:8082" → ["localhost:9091", "localhost:9092", "localhost:9093"]
//   - "example.com:8080" → ["example.com:9090"]
func autoConfigureGRPCServers(serverAddr string) []string {
	// Parse comma-separated server list
	servers := strings.Split(serverAddr, ",")
	for i := range servers {
		servers[i] = strings.TrimSpace(servers[i])
	}

	if len(servers) == 0 {
		return []string{"localhost:9091"}
	}

	// Check if we're dealing with local cluster ports
	isLocalCluster := false
	for _, server := range servers {
		if strings.Contains(server, "localhost") || strings.Contains(server, "127.0.0.1") {
			// Check if it's using standard HTTP or gRPC cluster ports
			if strings.Contains(server, ":8081") || strings.Contains(server, ":8082") || strings.Contains(server, ":8083") ||
				strings.Contains(server, ":9091") || strings.Contains(server, ":9092") || strings.Contains(server, ":9093") {
				isLocalCluster = true
				break
			}
		}
	}

	// If it's a local cluster with standard ports, return all three gRPC servers for auto-discovery
	if isLocalCluster {
		return []string{"localhost:9091", "localhost:9092", "localhost:9093"}
	}

	// Otherwise, convert HTTP ports to gRPC ports
	result := make([]string, 0, len(servers))
	for _, server := range servers {
		result = append(result, convertToGRPCAddr(server))
	}
	return result
}

// convertToGRPCAddr converts a single server address from HTTP to gRPC port
// Pattern: 808x → 909x (e.g., 8081 → 9091)
func convertToGRPCAddr(addr string) string {
	// Split host and port
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return addr // Return as-is if not in host:port format
	}

	host := parts[0]
	port := parts[1]

	// Check if it's an HTTP port (808x pattern)
	if len(port) == 4 && port[0:3] == "808" {
		// Convert 808x to 909x
		grpcPort := "909" + string(port[3])
		return host + ":" + grpcPort
	}

	// Check if it's the default HTTP port (8080)
	if port == "8080" {
		return host + ":9090"
	}

	// Already a gRPC port or custom port, return as-is
	return addr
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

	printSuccess(fmt.Sprintf("Put key '%s' (%d bytes)", key, len(value)))
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

	printSuccess(fmt.Sprintf("Deleted key '%s'", key))
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
		printSuccess("Server is healthy")
	} else {
		printWarning(fmt.Sprintf("Server status: %s", health.Status))
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
		printSuccess("Server is ready")
	} else {
		printWarning(fmt.Sprintf("Server status: %s", ready.Status))
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

	printSuccess(fmt.Sprintf("Snapshot created: %s", snap.Snapshot))
	if snap.Message != "" {
		printInfo(snap.Message)
	}

	return nil
}

// gRPC command implementations
func cmdGetGRPC(ctx context.Context, c *client.GRPCClient) error {
	if flag.NArg() < 2 {
		return fmt.Errorf("usage: kvcli get <key>")
	}

	key := flag.Arg(1)

	// Default to stale read for performance, can add --consistent flag later
	value, found, err := c.Get(ctx, key, false)
	if err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("key not found: %s", key)
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

func cmdPutGRPC(ctx context.Context, c *client.GRPCClient) error {
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

	printSuccess(fmt.Sprintf("Put key '%s' (%d bytes)", key, len(value)))
	return nil
}

func cmdDeleteGRPC(ctx context.Context, c *client.GRPCClient) error {
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

	printSuccess(fmt.Sprintf("Deleted key '%s'", key))
	return nil
}

func cmdListGRPC(ctx context.Context, c *client.GRPCClient) error {
	// Parse optional flags
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	prefix := fs.String("prefix", "", "Key prefix filter")
	limit := fs.Int("limit", 0, "Maximum number of keys")
	fs.Parse(flag.Args()[1:])

	keys, err := c.List(ctx, *prefix, *limit)
	if err != nil {
		return err
	}

	if *jsonOutput {
		output := map[string]interface{}{
			"keys":  keys,
			"count": len(keys),
		}
		return printJSON(output)
	}

	if len(keys) == 0 {
		printInfo("No keys found")
		return nil
	}

	printInfo(fmt.Sprintf("Found %d keys:", len(keys)))
	for _, key := range keys {
		fmt.Printf("  %s\n", colorize(colorCyan, key))
	}

	return nil
}

func cmdStatsGRPC(ctx context.Context, c *client.GRPCClient) error {
	stats, err := c.GetStats(ctx)
	if err != nil {
		return err
	}

	if *jsonOutput {
		return printJSON(stats)
	}

	printHeader("Server Statistics (gRPC)")
	fmt.Printf("  Keys:        %s\n", colorize(colorBlue, fmt.Sprintf("%d", stats.KeyCount)))
	fmt.Printf("  Gets:        %s\n", colorize(colorGreen, fmt.Sprintf("%d", stats.GetCount)))
	fmt.Printf("  Puts:        %s\n", colorize(colorYellow, fmt.Sprintf("%d", stats.PutCount)))
	fmt.Printf("  Deletes:     %s\n", colorize(colorRed, fmt.Sprintf("%d", stats.DeleteCount)))
	fmt.Printf("  Raft State:  %s\n", colorize(colorCyan, stats.RaftState))
	fmt.Printf("  Raft Leader: %s\n", colorize(colorGreen, stats.RaftLeader))

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
    health              Check server health (HTTP only)
    ready               Check server readiness (HTTP only)
    snapshot            Trigger manual snapshot (HTTP only)

OPTIONS:
    --server=<addr>     Server address (default: localhost:8080)
                        For gRPC: HTTP ports auto-convert (8081→9091)
    --protocol=<proto>  Protocol to use: http or grpc (default: http)
    --timeout=<dur>     Request timeout (default: 10s)
    --json              Output in JSON format
    --no-color          Disable colored output

AUTO-CONFIGURATION (gRPC):
    The CLI automatically configures gRPC servers based on HTTP addresses:
    - HTTP port 808x → gRPC port 909x (8081→9091, 8082→9092, etc.)
    - Local cluster detected → Auto-adds all 3 servers for discovery
    - Already gRPC ports → Used as-is

EXAMPLES:
    # Using HTTP (default)
    kvcli put user:1 "Alice"
    kvcli get user:1
    kvcli list --prefix=user:
    kvcli delete user:1
    kvcli stats

    # Using gRPC with HTTP port (auto-converts to gRPC and adds all servers)
    kvcli --protocol=grpc --server=localhost:8081 put user:1 "Alice"
    # ↑ Auto-configures to: localhost:9091,localhost:9092,localhost:9093

    # Using gRPC with gRPC port (auto-adds all servers)
    kvcli --protocol=grpc --server=localhost:9091 get user:1
    # ↑ Auto-configures to: localhost:9091,localhost:9092,localhost:9093

    # Set default protocol via environment variable
    export RAFTKV_PROTOCOL=grpc
    export RAFTKV_SERVER=localhost:8081
    kvcli put user:1 "Alice"
    kvcli get user:1
    # ↑ Auto-converts to gRPC ports and adds all servers

ENVIRONMENT VARIABLES:
    RAFTKV_SERVER       Server address (overrides --server)
    RAFTKV_PROTOCOL     Protocol to use: http or grpc (overrides --protocol)
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
	if proto := os.Getenv("RAFTKV_PROTOCOL"); proto != "" {
		*protocol = proto
	}
	if t := os.Getenv("RAFTKV_TIMEOUT"); t != "" {
		if duration, err := time.ParseDuration(t); err == nil {
			*timeout = duration
		}
	}
}
