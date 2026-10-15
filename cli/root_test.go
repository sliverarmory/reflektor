package main

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/sliverarmory/reflektor/bof"
)

func TestParseMachineWordArgs(t *testing.T) {
	got, err := parseMachineWordArgs([]string{"08", "0x10", "7"})
	if err != nil {
		t.Fatalf("parse arguments: %v", err)
	}
	for i, want := range []uintptr{8, 16, 7} {
		if got[i] != want {
			t.Fatalf("argument %d = %d, want %d", i, got[i], want)
		}
	}
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "too many", args: []string{"1", "2", "3", "4"}, want: "at most 3"},
		{name: "invalid", args: []string{"abc"}, want: "parse --arg 1"},
		{name: "overflow", args: []string{strconv.FormatUint(^uint64(0), 10) + "0"}, want: "parse --arg 1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseMachineWordArgs(test.args); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseMachineWordArgs(%q) error = %v, want %q", test.args, err, test.want)
			}
		})
	}
}

func TestAppendBOFArgument(t *testing.T) {
	var arguments bof.Arguments
	for _, raw := range []string{"int32:0x12345678", "int16:0x1234", "string:bof"} {
		if err := appendBOFArgument(&arguments, raw); err != nil {
			t.Fatalf("append %q: %v", raw, err)
		}
	}
	want := []byte{14, 0, 0, 0, 0x78, 0x56, 0x34, 0x12, 0x34, 0x12, 4, 0, 0, 0, 'b', 'o', 'f', 0}
	if got := arguments.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("packed BOF arguments = %x, want %x", got, want)
	}
	if value, err := parseBOFInteger("08", 16); err != nil || value != 8 {
		t.Fatalf("decimal with leading zero = (%d, %v), want 8", value, err)
	}
	for _, raw := range []string{"missing-colon", "int16:32768", "int32:0xz", "hex:odd", "unknown:value", "string:\x00"} {
		if err := appendBOFArgument(&arguments, raw); err == nil {
			t.Errorf("append %q unexpectedly succeeded", raw)
		}
	}
}
