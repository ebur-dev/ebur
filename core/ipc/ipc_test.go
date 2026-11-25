package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/ebur-dev/ebur/core/paths"
)

type hinted struct{}

func (hinted) Error() string { return "port 80 is in use" }
func (hinted) Hint() string  { return "close IIS" }

func handler(_ context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "echo":
		var p map[string]string
		json.Unmarshal(params, &p)
		return p, nil
	case "nil":
		return nil, nil
	case "fail":
		return nil, hinted{}
	case "plain":
		return nil, errors.New("plain failure")
	case "badresult":
		return make(chan int), nil
	}
	return nil, errors.New("unknown method " + method)
}

func start(t *testing.T) paths.Root {
	t.Helper()
	root, _ := paths.New(t.TempDir())
	ln, err := Listen(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go Serve(ctx, ln, handler)
	t.Cleanup(func() { cancel(); ln.Close() })
	return root
}

func call(root paths.Root, method string, params, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return Call(ctx, root, method, params, out)
}

func TestRoundTrip(t *testing.T) {
	root := start(t)
	var out map[string]string
	if err := call(root, "echo", map[string]string{"php": "8.4"}, &out); err != nil {
		t.Fatal(err)
	}
	if out["php"] != "8.4" {
		t.Fatalf("got %v", out)
	}
	if err := call(root, "nil", nil, &out); err != nil {
		t.Fatal(err)
	}
}

func TestErrorsCarryHints(t *testing.T) {
	root := start(t)
	var e *Error
	if err := call(root, "fail", nil, nil); !errors.As(err, &e) || e.Hint != "close IIS" || e.Error() != "port 80 is in use" {
		t.Fatalf("got %#v", err)
	}
	if err := call(root, "plain", nil, nil); !errors.As(err, &e) || e.Hint != "" {
		t.Fatalf("got %#v", err)
	}
	if err := call(root, "badresult", nil, nil); !errors.As(err, &e) {
		t.Fatalf("got %#v", err)
	}
	if err := call(root, "echo", make(chan int), nil); err == nil {
		t.Fatal("unencodable params accepted")
	}
}

func TestNotRunning(t *testing.T) {
	root, _ := paths.New(t.TempDir())
	if err := call(root, "echo", nil, nil); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("want ErrNotRunning, got %v", err)
	}
}

func TestSecondHostRefused(t *testing.T) {
	root := start(t)
	if ln, err := Listen(root); err == nil {
		ln.Close()
		t.Fatal("second listener on the same root was allowed")
	}
}

func TestRootsAreIsolated(t *testing.T) {
	a, _ := paths.New(`C:\ebur`)
	b, _ := paths.New(`C:\ebur-dev`)
	c, _ := paths.New(`c:\EBUR`)
	if endpoint(a) == endpoint(b) {
		t.Fatal("different roots share an endpoint")
	}
	if runtime.GOOS == "windows" && endpoint(a) != endpoint(c) {
		t.Fatal("same root with different case got different endpoints")
	}
}
