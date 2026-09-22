package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	_ = godotenv.Load()
	endpoint := strings.TrimSpace(os.Getenv("BREWMARK_MCP_URL"))
	if endpoint == "" {
		fmt.Fprintln(os.Stderr, "BREWMARK_MCP_URL is required")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "brewmark-mcp-diagnostic", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "MCP connection failed")
		os.Exit(1)
	}
	defer session.Close()
	if err := session.Ping(ctx, nil); err != nil {
		fmt.Fprintln(os.Stderr, "MCP ping failed")
		os.Exit(1)
	}
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "MCP tools/list failed")
		os.Exit(1)
	}
	tools := result.Tools
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	if len(tools) != 4 {
		fmt.Fprintln(os.Stderr, "MCP returned unexpected tools")
		os.Exit(1)
	}
	for _, tool := range tools {
		if tool.Name == "" || tool.Description == "" {
			fmt.Fprintln(os.Stderr, "MCP returned invalid tool")
			os.Exit(1)
		}
		fmt.Printf("%s: %s\n", tool.Name, tool.Description)
	}
}
