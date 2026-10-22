package main

import "testing"

func TestParseArgs(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want options
	}{
		{"defaults", nil, options{launch: true}},
		{"as the extension passes them", []string{"--port", "47490", "--launch=false"}, options{port: 47490}},
		{"sheetdiver mcp's argument", []string{"mcp", "-port", "47490"}, options{port: 47490, launch: true}},
		{"the extension's default port", []string{"--port", "0", "--launch=true"}, options{launch: true}},
		// Settings that come through blank or odd fall back to the
		// defaults instead of ending the session with a usage error.
		{"blank", []string{"--port", "", "--launch="}, options{launch: true}},
		{"odd", []string{"--port", "${user_config.port}", "--launch=${user_config.launch}"}, options{launch: true}},
		{"a whole number written as a float", []string{"--port", "47480.0"}, options{port: 47480, launch: true}},
		{"a fraction", []string{"--port", "47480.5"}, options{launch: true}},
		{"app and version", []string{"-app", `C:\SheetDiver\sheetdiver.exe`, "-version"}, options{app: `C:\SheetDiver\sheetdiver.exe`, launch: true, version: true}},
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
