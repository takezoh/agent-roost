package proto

import "fmt"

// BuildCommand constructs a typed Command from a command name and
// string arguments. Used by the `roost send` CLI subcommand to
// translate user input into the typed IPC message.
func BuildCommand(name string, args map[string]string) (Command, error) {
	switch name {
	case CmdNamePreviewProject:
		return CmdPreviewProject{Project: args["project"]}, nil
	case CmdNamePreviewSession:
		id := args["session_id"]
		if id == "" {
			return nil, fmt.Errorf("session_id required")
		}
		return CmdPreviewSession{SessionID: id}, nil
	case CmdNameSwitchSession:
		id := args["session_id"]
		if id == "" {
			return nil, fmt.Errorf("session_id required")
		}
		return CmdSwitchSession{SessionID: id}, nil
	case CmdNameFocusPane:
		pane := args["pane"]
		if pane == "" {
			return nil, fmt.Errorf("pane required")
		}
		return CmdFocusPane{Pane: pane}, nil
	case CmdNameListSessions:
		return CmdListSessions{}, nil
	case CmdNameShutdown:
		return CmdShutdown{}, nil
	case CmdNameDetach:
		return CmdDetach{}, nil
	default:
		return nil, fmt.Errorf("unknown command: %s", name)
	}
}
