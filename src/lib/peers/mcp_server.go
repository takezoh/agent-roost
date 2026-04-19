package peers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/takezoh/agent-roost/proto"
)

func runMCPServer() error {
	server := mcp.NewServer(&mcp.Implementation{Name: "roost-peers", Version: "1.0"}, nil)

	type listPeersArgs struct {
		Scope string `json:"scope" jsonschema:"scope: workspace, project, or all,default=workspace"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_peers",
		Description: "List peer agent frames visible from this frame",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listPeersArgs) (*mcp.CallToolResult, any, error) {
		scope := args.Scope
		if scope == "" {
			scope = "workspace"
		}
		frameID := callerFrameID()
		client, err := dialDaemon()
		if err != nil {
			return nil, nil, fmt.Errorf("dial daemon: %w", err)
		}
		defer client.Close()

		peers, err := client.PeerList(frameID, scope)
		if err != nil {
			return nil, nil, fmt.Errorf("peer.list: %w", err)
		}
		b, err := json.Marshal(peers)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(b)},
			},
		}, nil, nil
	})

	type peerSendArgs struct {
		To      string `json:"to" jsonschema:"target frame_id"`
		Text    string `json:"text" jsonschema:"message text"`
		ReplyTo string `json:"reply_to,omitempty" jsonschema:"optional message id to reply to"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "peer_send",
		Description: "Send a message to a peer agent frame",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args peerSendArgs) (*mcp.CallToolResult, any, error) {
		frameID := callerFrameID()
		client, err := dialDaemon()
		if err != nil {
			return nil, nil, fmt.Errorf("dial daemon: %w", err)
		}
		defer client.Close()

		if err := client.PeerSend(frameID, args.To, args.Text, args.ReplyTo); err != nil {
			return nil, nil, fmt.Errorf("peer.send: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "sent"},
			},
		}, nil, nil
	})

	type setSummaryArgs struct {
		Summary string `json:"summary" jsonschema:"brief description of what this agent is currently doing"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_summary",
		Description: "Update this frame's peer summary visible to other agents",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args setSummaryArgs) (*mcp.CallToolResult, any, error) {
		frameID := callerFrameID()
		client, err := dialDaemon()
		if err != nil {
			return nil, nil, fmt.Errorf("dial daemon: %w", err)
		}
		defer client.Close()

		if err := client.PeerSetSummary(frameID, args.Summary); err != nil {
			return nil, nil, fmt.Errorf("peer.set_summary: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "ok"},
			},
		}, nil, nil
	})

	type checkMessagesArgs struct{}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "check_messages",
		Description: "Drain and return inbox messages for the current frame (polling fallback)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args checkMessagesArgs) (*mcp.CallToolResult, any, error) {
		frameID := callerFrameID()
		client, err := dialDaemon()
		if err != nil {
			return nil, nil, fmt.Errorf("dial daemon: %w", err)
		}
		defer client.Close()

		msgs, err := client.PeerDrainInbox(frameID)
		if err != nil {
			return nil, nil, fmt.Errorf("peer.drain_inbox: %w", err)
		}
		b, err := json.Marshal(struct {
			Messages []proto.PeerMessage `json:"messages"`
			Count    int                 `json:"count"`
		}{Messages: msgs, Count: len(msgs)})
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(b)},
			},
		}, nil, nil
	})

	return server.Run(context.Background(), &mcp.StdioTransport{})
}
