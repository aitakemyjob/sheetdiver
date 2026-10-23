package main

import "testing"

func TestParseArgs(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want options
	}{
		{"defaults", nil, options{}},
		{"as the extension passes them", []string{"--port", "47490"}, options{port: 47490}},
		{"sheetdiver mcp's arguments", []string{"mcp", "-port", "47490", "-standalone"}, options{port: 47490, standalone: true}},
		{"the extension's default port", []string{"--port", "0"}, options{}},
		// Settings that come through blank or odd fall back to the
		// defaults instead of ending the session with a usage error.
		{"blank", []string{"--port", ""}, options{}},
		{"odd", []string{"--port", "${user_config.port}"}, options{}},
		{"a whole number written as a float", []string{"--port", "47480.0"}, options{port: 47480}},
		{"a fraction", []string{"--port", "47480.5"}, options{}},
		// Extensions saved by earlier versions pass these.
		{"retired flags", []string{"--port", "47490", "--launch=true", "--app", `C:\SheetDiver\sheetdiver.exe`}, options{port: 47490}},
		{"version", []string{"-version"}, options{version: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseArgs(tc.args)
			if err != nil || got != tc.want {
				t.Errorf("parseArgs(%q) = %+v, %v; want %+v", tc.args, got, err, tc.want)
			}
		})
	}
	if _, err := parseArgs([]string{"-nope"}); err == nil {
		t.Error("an unknown flag is still an error")
	}
}
