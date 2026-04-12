package cli

import (
	"errors"
	"testing"
)

func TestDispatch(t *testing.T) {
	const name = "__test_dispatch__"
	wantErr := errors.New("boom")
	Register(name, "test", func(args []string) error {
		if len(args) != 1 || args[0] != "x" {
			t.Fatalf("args = %v", args)
		}
		return wantErr
	})
	defer delete(commands, name)

	handled, err := Dispatch([]string{name, "x"})
	if !handled {
		t.Fatal("handled = false, want true")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestDispatchUnknown(t *testing.T) {
	handled, err := Dispatch([]string{"__missing__"})
	if handled {
		t.Fatal("handled = true, want false")
	}
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}
