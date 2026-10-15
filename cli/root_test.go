package main

import (
	"strconv"
	"strings"
	"testing"
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
