package driver

import "regexp"

// Registry はコマンド名から Driver を引くための不変マップ。
// 未知コマンドには fallback Driver を返す（nil は返さない）。
type Registry struct {
	drivers  map[string]Driver
	patterns map[string]*regexp.Regexp
	fallback Driver
}

// NewRegistry は drivers を登録した Registry を返す。
// 未知コマンドには fallback が使われる。
func NewRegistry(drivers []Driver, fallback Driver) *Registry {
	r := &Registry{
		drivers:  make(map[string]Driver, len(drivers)),
		patterns: make(map[string]*regexp.Regexp, len(drivers)+1),
		fallback: fallback,
	}
	for _, d := range drivers {
		r.drivers[d.Name()] = d
		r.patterns[d.Name()] = regexp.MustCompile(d.PromptPattern())
	}
	r.patterns[""] = regexp.MustCompile(fallback.PromptPattern())
	return r
}

// Get はコマンド名に対応する Driver を返す。未知コマンドは fallback。
func (r *Registry) Get(command string) Driver {
	if d, ok := r.drivers[command]; ok {
		return d
	}
	return r.fallback
}

// CompiledPattern はコマンド名に対応するコンパイル済み正規表現を返す。
// 未知コマンドは fallback のパターンを返す。
func (r *Registry) CompiledPattern(command string) *regexp.Regexp {
	if p, ok := r.patterns[command]; ok {
		return p
	}
	return r.patterns[""]
}

// DefaultRegistry は既知コマンド用の Registry を返す。
func DefaultRegistry() *Registry {
	drivers := []Driver{
		Claude{},
		NewGeneric("gemini"),
		NewGeneric("codex"),
		NewGeneric("bash"),
	}
	return NewRegistry(drivers, NewGeneric(""))
}
