package bof

import (
	"bytes"
	"errors"
	"testing"

	"github.com/sliverarmory/reflektor/internal/bofloader"
)

type scriptedOutputLoader struct {
	outputs      []bofloader.Output
	err          error
	receivedStop <-chan struct{}
}

func (loader *scriptedOutputLoader) Execute([]byte) ([]bofloader.Output, error) {
	return loader.outputs, loader.err
}

func (loader *scriptedOutputLoader) ExecuteWithOptions(_ []byte, emit func(bofloader.Output), stop <-chan struct{}) error {
	loader.receivedStop = stop
	if emit != nil {
		for _, output := range loader.outputs {
			emit(output)
		}
	}
	return loader.err
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

func TestObjectExecuteWithOutputDeliversOwnedRecordsBeforeError(t *testing.T) {
	terminalErr := errors.New("terminal execution error")
	loader := &scriptedOutputLoader{
		outputs: []bofloader.Output{
			{Type: OutputOEM, Data: []byte{0xff, 0xfe, 0x80}},
			{Type: -7, Data: nil},
		},
		err: terminalErr,
	}
	object := &Object{loader: loader}
	var delivered []Output
	err := object.ExecuteWithOutput(nil, func(output Output) {
		delivered = append(delivered, output)
	})
	if !errors.Is(err, terminalErr) {
		t.Fatalf("ExecuteWithOutput() error = %v, want terminal execution error", err)
	}
	if len(delivered) != 2 || delivered[0].Type != OutputOEM || !bytes.Equal(delivered[0].Data, []byte{0xff, 0xfe, 0x80}) || delivered[1].Type != -7 || len(delivered[1].Data) != 0 {
		t.Fatalf("delivered records = %#v", delivered)
	}
	loader.outputs[0].Data[0] = 0
	if delivered[0].Data[0] != 0xff {
		t.Fatal("delivered record aliases loader data")
	}
	if err := object.ExecuteWithOutput(nil, nil); !errors.Is(err, terminalErr) {
		t.Fatalf("ExecuteWithOutput(nil emit) error = %v, want terminal execution error", err)
	}
	if loader.receivedStop != nil {
		t.Fatal("ExecuteWithOutput supplied an unexpected stop channel")
	}
}

func TestObjectExecuteWithOptionsForwardsStopRequest(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	loader := &scriptedOutputLoader{outputs: []bofloader.Output{{Type: OutputDefault, Data: []byte("started")}}}
	object := &Object{loader: loader}
	var delivered []Output
	err := object.ExecuteWithOptions(nil, ExecuteOptions{
		OnOutput: func(output Output) { delivered = append(delivered, output) },
		Stop:     stop,
	})
	if err != nil {
		t.Fatalf("ExecuteWithOptions() error = %v", err)
	}
	if loader.receivedStop != stop {
		t.Fatal("ExecuteWithOptions did not forward the stop channel")
	}
	if len(delivered) != 1 || delivered[0].Type != OutputDefault || string(delivered[0].Data) != "started" {
		t.Fatalf("delivered records = %#v", delivered)
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
