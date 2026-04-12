package cli

import "sort"

// Subcommand holds a registered subcommand.
type Subcommand struct {
	Run  func(args []string) error
	Help string // one-line description
}

var commands = map[string]Subcommand{}

func Has(name string) bool {
	_, ok := commands[name]
	return ok
}

// Register adds a subcommand to the registry.
func Register(name string, help string, fn func(args []string) error) {
	commands[name] = Subcommand{Run: fn, Help: help}
}

// Dispatch tries to run a registered subcommand.
func Dispatch(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	cmd, ok := commands[args[0]]
	if !ok {
		return false, nil
	}
	return true, cmd.Run(args[1:])
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
