package reflektor_test

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/sliverarmory/reflektor/bof"
)

func TestBOFHostAdapterNativeCallbacks(t *testing.T) {
	target, ok := nativeBOFTarget()
	if !ok {
		t.Fatalf("missing BOF fixture target for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	path := buildBOFSource(t, t.TempDir(), target, "host_adapter_fixture", "host_adapter_fixture.c")
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if object, err := bof.LoadWithOptions(image, bof.NewHostAdapter(bof.HostServices{}).Apply(bof.LoadOptions{})); err == nil || object != nil || !strings.Contains(err.Error(), "no service") {
		if object != nil {
			_ = object.Close()
		}
		t.Fatalf("load without services = %#v, %v", object, err)
	}
	var use, revert, admin, spawn int
	adapter := bof.NewHostAdapter(bof.HostServices{
		UseToken: func(handle bof.TokenHandle) (bool, error) {
			use++
			if handle != 0x345 {
				return false, errors.New("wrong token handle")
			}
			return true, nil
		},
		RevertToken: func() error { revert++; return nil },
		IsAdmin:     func() (bool, error) { admin++; return true, nil },
		GetSpawnTo: func(x86 bool) ([]byte, error) {
			spawn++
			if !x86 {
				return nil, errors.New("wrong architecture")
			}
			return []byte("host"), nil
		},
	})
	options := adapter.Apply(bof.LoadOptions{})
	object, err := bof.LoadWithOptions(image, options)
	if err != nil {
		t.Fatal(err)
	}
	defer object.Close()
	for run := 1; run <= 2; run++ {
		outputs, err := object.Execute(nil)
		if err != nil {
			t.Fatalf("Execute run %d: %v", run, err)
		}
		if len(outputs) != 1 || outputs[0].Type != bof.OutputDefault || string(outputs[0].Data) != "host-adapter-ok" {
			t.Fatalf("run %d outputs = %#v", run, outputs)
		}
		if use != run || revert != run || admin != run || spawn != run {
			t.Fatalf("run %d service counts = %d, %d, %d, %d", run, use, revert, admin, spawn)
		}
	}
	outputs, err := object.Execute([]byte{1})
	if err != nil || len(outputs) != 1 || string(outputs[0].Data) != "host-adapter-ok" {
		t.Fatalf("automatic token cleanup execution = %#v, %v", outputs, err)
	}
	if use != 3 || revert != 3 || admin != 3 || spawn != 3 {
		t.Fatalf("automatic token cleanup counts = %d, %d, %d, %d", use, revert, admin, spawn)
	}
	outputs, err = object.Execute([]byte{1, 2})
	if err != nil || len(outputs) != 1 || string(outputs[0].Data) != "host-adapter-ok" {
		t.Fatalf("repeated token-use execution = %#v, %v", outputs, err)
	}
	if use != 5 || revert != 4 || admin != 4 || spawn != 4 {
		t.Fatalf("repeated token-use cleanup counts = %d, %d, %d, %d", use, revert, admin, spawn)
	}
	if err := object.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := object.Execute(nil); !errors.Is(err, bof.ErrClosed) {
		t.Fatalf("execute after close = %v", err)
	}

	for _, test := range []struct {
		name     string
		useToken func(bof.TokenHandle) (bool, error)
		want     string
	}{
		{name: "service error", useToken: func(bof.TokenHandle) (bool, error) {
			return false, errors.New("host denied token")
		}, want: "host denied token"},
		{name: "service panic", useToken: func(bof.TokenHandle) (bool, error) {
			panic("host callback panic")
		}, want: "host callback panic"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reverts := 0
			failedAdapter := bof.NewHostAdapter(bof.HostServices{
				UseToken:    test.useToken,
				RevertToken: func() error { reverts++; return nil },
				IsAdmin:     func() (bool, error) { return true, nil },
				GetSpawnTo:  func(bool) ([]byte, error) { return []byte("host"), nil },
			})
			failed, err := bof.LoadWithOptions(image, failedAdapter.Apply(bof.LoadOptions{}))
			if err != nil {
				t.Fatal(err)
			}
			defer failed.Close()
			outputs, err := failed.Execute(nil)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Execute error = %v, want %q", err, test.want)
			}
			if len(outputs) != 1 || outputs[0].Type != bof.OutputError || string(outputs[0].Data) != "host-adapter-failed" {
				t.Fatalf("outputs despite error = %#v", outputs)
			}
			if reverts != 1 {
				t.Fatalf("cleanup calls = %d, want one after failed token use", reverts)
			}
		})
	}

	t.Run("spawn path overflow", func(t *testing.T) {
		failedAdapter := bof.NewHostAdapter(bof.HostServices{
			UseToken:    func(bof.TokenHandle) (bool, error) { return true, nil },
			RevertToken: func() error { return nil },
			IsAdmin:     func() (bool, error) { return true, nil },
			GetSpawnTo: func(bool) ([]byte, error) {
				return []byte("path that is too long for the destination buffer"), nil
			},
		})
		failed, err := bof.LoadWithOptions(image, failedAdapter.Apply(bof.LoadOptions{}))
		if err != nil {
			t.Fatal(err)
		}
		defer failed.Close()
		outputs, err := failed.Execute(nil)
		if err == nil || !strings.Contains(err.Error(), "spawn path") {
			t.Fatalf("Execute error = %v, want spawn path error", err)
		}
		if len(outputs) != 1 || outputs[0].Type != bof.OutputError || string(outputs[0].Data) != "host-adapter-failed" {
			t.Fatalf("outputs despite error = %#v", outputs)
		}
	})

	t.Run("cleanup panic", func(t *testing.T) {
		failedAdapter := bof.NewHostAdapter(bof.HostServices{
			UseToken:    func(bof.TokenHandle) (bool, error) { return true, nil },
			RevertToken: func() error { panic("revert failed") },
			IsAdmin:     func() (bool, error) { return true, nil },
			GetSpawnTo:  func(bool) ([]byte, error) { return []byte("host"), nil },
		})
		failed, err := bof.LoadWithOptions(image, failedAdapter.Apply(bof.LoadOptions{}))
		if err != nil {
			t.Fatal(err)
		}
		defer failed.Close()
		outputs, err := failed.Execute([]byte{1})
		if err == nil || !strings.Contains(err.Error(), "host cleanup panic") {
			t.Fatalf("Execute error = %v, want cleanup panic", err)
		}
		if len(outputs) != 1 || outputs[0].Type != bof.OutputDefault || string(outputs[0].Data) != "host-adapter-ok" {
			t.Fatalf("outputs despite cleanup panic = %#v", outputs)
		}
	})
}
