package main

import (
	"context"
	"path/filepath"
	"strings"
)

// missingGuidance is reported (as InstallStatus.Error) when tokscale is
// neither configured nor on PATH. The plugin never falls back to fetching
// tokscale from npm at request time: that would download and execute remote
// code on every webhook call. Operators who want npx must type it into the
// `command` setting themselves.
const missingGuidance = "tokscale is not installed: install the tokscale CLI on the Kandev host's PATH, " +
	"or set its command in Settings → Plugins → Session Cost"

// Command sources, reported in InstallStatus.Source so the UI can explain
// where the tokscale invocation came from.
const (
	sourceSettings = "settings" // operator-configured command
	sourcePath     = "path"     // tokscale binary found on PATH
	sourceMissing  = "missing"  // neither configured nor on PATH; nothing runs
)

// resolvedCommand is the argv the plugin will run tokscale with, plus where
// that argv came from.
type resolvedCommand struct {
	Argv   []string
	Source string
}

// resolveCommand picks the tokscale invocation: the operator-configured
// command wins, then the absolute path of a `tokscale` binary on PATH.
// Otherwise the command is "missing" (empty Argv) and callers must not spawn
// anything — there is deliberately no implicit npx fallback. lookPath is
// exec.LookPath in production, injected for tests.
func resolveCommand(configured string, lookPath func(string) (string, error)) resolvedCommand {
	if argv := strings.Fields(configured); len(argv) > 0 {
		return resolvedCommand{Argv: argv, Source: sourceSettings}
	}
	if path, err := lookPath("tokscale"); err == nil && filepath.IsAbs(path) {
		return resolvedCommand{Argv: []string{path}, Source: sourcePath}
	}
	return resolvedCommand{Source: sourceMissing}
}

// missingStatus is the install status for an unresolved command. No process
// is spawned to produce it.
func missingStatus() InstallStatus {
	return InstallStatus{Source: sourceMissing, Error: missingGuidance}
}

// InstallStatus reports whether the resolved command actually runs, and as
// what version. Embedded in every session-cost payload so the UI can render
// setup guidance from the same shape it always reads.
type InstallStatus struct {
	// Command is the resolved argv joined for display, e.g. "/usr/local/bin/tokscale".
	// Empty when Source is "missing".
	Command string `json:"command"`
	// Source is where the command came from: "settings", "path" or "missing".
	Source    string `json:"source"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

// runner executes a command and returns its stdout — exec.CommandContext in
// production (see newPlugin), injected for tests.
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// commandDisplay renders the resolved argv for humans ("/usr/local/bin/tokscale").
func commandDisplay(cmd resolvedCommand) string {
	return strings.Join(cmd.Argv, " ")
}

// probeInstall checks the resolved command works by running `--version` and
// parsing the version out of its output ("tokscale 4.15.1").
func probeInstall(ctx context.Context, cmd resolvedCommand, run runner) InstallStatus {
	if len(cmd.Argv) == 0 {
		return missingStatus()
	}
	status := InstallStatus{Command: commandDisplay(cmd), Source: cmd.Source}
	out, err := run(ctx, cmd.Argv[0], append(cmd.Argv[1:], "--version")...)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Installed = true
	status.Version = parseVersion(string(out))
	return status
}

// parseVersion extracts "4.15.1" from tokscale's `--version` output, which may
// be preceded by launcher noise (e.g. an operator-configured npx command).
// Empty when no version line is found.
func parseVersion(out string) string {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "tokscale" {
			return fields[1]
		}
	}
	return ""
}
