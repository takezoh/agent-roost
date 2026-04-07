package lib

import "sort"

// Subcommand holds a registered subcommand.
type Subcommand struct {
	Run  func(args []string)
	Help string // one-line description
}

var commands = map[string]Subcommand{}

// Register adds a subcommand to the registry.
func Register(name string, help string, fn func(args []string)) {
	commands[name] = Subcommand{Run: fn, Help: help}
}

// Dispatch tries to run a registered subcommand. Returns true if handled.
func Dispatch(args []string) bool {
	if len(args) == 0 {
		return false
	}
	cmd, ok := commands[args[0]]
	if !ok {
		return false
	}
	cmd.Run(args[1:])
	return true
}

// RegisteredHelp returns sorted name-help pairs for all registered subcommands.
func RegisteredHelp() [][2]string {
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	pairs := make([][2]string, len(names))
	for i, name := range names {
		pairs[i] = [2]string{name, commands[name].Help}
	}
	return pairs
}
