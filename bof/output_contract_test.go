package bof

import (
	"bytes"
	"errors"
	"testing"

	"github.com/sliverarmory/reflektor/internal/bofloader"
)

type scriptedOutputLoader struct {
	outputs []bofloader.Output
	err     error
}

func (loader *scriptedOutputLoader) Execute([]byte) ([]bofloader.Output, error) {
	return loader.outputs, loader.err
}

func (*scriptedOutputLoader) Close() error {
	return nil
}

func TestObjectExecutePreservesTypedOutputWithError(t *testing.T) {
	terminalErr := errors.New("terminal execution error")
	loader := &scriptedOutputLoader{
		outputs: []bofloader.Output{
			{Type: OutputDefault, Data: []byte("default")},
			{Type: OutputError, Data: []byte("error")},
			{Type: OutputOEM, Data: []byte{0xff, 0xfe, 0x80}},
			{Type: OutputUTF8, Data: []byte("snowman:\u2603")},
			{Type: OutputDefault, Data: nil},
			{Type: -7, Data: []byte("unknown")},
		},
		err: terminalErr,
	}
	object := &Object{loader: loader}

	outputs, err := object.Execute(nil)
	if !errors.Is(err, terminalErr) {
		t.Fatalf("Execute() error = %v, want terminal execution error", err)
	}
	want := []Output{
		{Type: OutputDefault, Data: []byte("default")},
		{Type: OutputError, Data: []byte("error")},
		{Type: OutputOEM, Data: []byte{0xff, 0xfe, 0x80}},
		{Type: OutputUTF8, Data: []byte("snowman:\u2603")},
		{Type: OutputDefault, Data: nil},
		{Type: -7, Data: []byte("unknown")},
	}
	if len(outputs) != len(want) {
		t.Fatalf("output count = %d, want %d: %#v", len(outputs), len(want), outputs)
	}
	for index := range outputs {
		if outputs[index].Type != want[index].Type || !bytes.Equal(outputs[index].Data, want[index].Data) {
			t.Fatalf("output %d = %#v, want %#v", index, outputs[index], want[index])
		}
	}
}

func TestOutputChannelValuesRemainCompatible(t *testing.T) {
	want := map[string]struct {
		got  int
		want int
	}{
		"default": {got: OutputDefault, want: 0x00},
		"error":   {got: OutputError, want: 0x0d},
		"OEM":     {got: OutputOEM, want: 0x1e},
		"UTF8":    {got: OutputUTF8, want: 0x20},
	}
	for name, channel := range want {
		if channel.got != channel.want {
			t.Errorf("%s output channel = %#x, want %#x", name, channel.got, channel.want)
		}
	}
}
