package main

import (
	"context"
	"errors"
	"testing"

	"github.com/kandev/kandev/pkg/pluginsdk"
	"github.com/stretchr/testify/require"
)

// failingRunner fails the test if the plugin ever spawns a process.
func failingRunner(t *testing.T) runner {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		t.Fatalf("runner must not be invoked, got %s %v", name, args)
		return nil, errors.New("unreachable")
	}
}

func TestResolveCommand_EmptyConfigAndNoPathIsMissing(t *testing.T) {
	cmd := resolveCommand("", noLookPath)
	require.Equal(t, sourceMissing, cmd.Source)
	require.Empty(t, cmd.Argv, "no implicit npx (or any other) fallback")
}

func TestResolveCommand_ConfiguredCommandWins(t *testing.T) {
	lookPath := func(string) (string, error) {
		t.Fatal("PATH lookup must not run when a command is configured")
		return "", nil
	}
	cmd := resolveCommand("/opt/bin/tokscale --flag", lookPath)
	require.Equal(t, sourceSettings, cmd.Source)
	require.Equal(t, []string{"/opt/bin/tokscale", "--flag"}, cmd.Argv)
}

func TestResolveCommand_ExplicitNpxStillAllowedWhenConfigured(t *testing.T) {
	cmd := resolveCommand("npx -y tokscale@4.15.1", noLookPath)
	require.Equal(t, sourceSettings, cmd.Source)
	require.Equal(t, []string{"npx", "-y", "tokscale@4.15.1"}, cmd.Argv)
}

func TestResolveCommand_PathHitResolvesAbsolutePath(t *testing.T) {
	lookPath := func(name string) (string, error) {
		require.Equal(t, "tokscale", name)
		return "/usr/local/bin/tokscale", nil
	}
	cmd := resolveCommand("", lookPath)
	require.Equal(t, sourcePath, cmd.Source)
	require.Equal(t, []string{"/usr/local/bin/tokscale"}, cmd.Argv)
}

func TestResolveCommand_RelativePathHitIsMissing(t *testing.T) {
	lookPath := func(string) (string, error) { return "./tokscale", nil }
	cmd := resolveCommand("", lookPath)
	require.Equal(t, sourceMissing, cmd.Source)
	require.Empty(t, cmd.Argv)
}

func TestHandleWebhook_MissingTokscaleSpawnsNothing(t *testing.T) {
	sessions := []pluginsdk.Session{session("kandev-sess", "abc-123")}
	p := newTestPlugin(nil, sessions, failingRunner(t))
	p.lookPath = noLookPath

	resp, err := p.HandleWebhook(context.Background(),
		webhookPost(webhookKeySessionCost, "task_id=task-1&active=kandev-sess"))
	require.NoError(t, err)
	require.Equal(t, int32(200), resp.Status)

	d := decode(t, resp.Body)
	require.False(t, d.Tokscale.Installed)
	require.Equal(t, sourceMissing, d.Tokscale.Source)
	require.Empty(t, d.Tokscale.Command)
	require.Contains(t, d.Tokscale.Error, "not installed")
	require.False(t, d.Found)
	require.Equal(t, "abc-123", d.ACPSessionID)
}

func TestHandleWebhook_AcceptsPOST(t *testing.T) {
	sessions := []pluginsdk.Session{session("kandev-sess", "abc-123")}
	p := newTestPlugin(map[string]any{"command": "/opt/bin/tokscale"}, sessions,
		modelsRunner([]byte(sampleSessionJSON), nil))

	resp, err := p.HandleWebhook(context.Background(),
		webhookPost(webhookKeySessionCost, "task_id=task-1&active=kandev-sess"))
	require.NoError(t, err)
	require.Equal(t, int32(200), resp.Status)
	d := decode(t, resp.Body)
	require.True(t, d.Found)
	require.Equal(t, sourceSettings, d.Tokscale.Source)
}

func TestHandleWebhook_RejectsOtherMethods(t *testing.T) {
	p := newTestPlugin(nil, nil, failingRunner(t))
	resp, err := p.HandleWebhook(context.Background(), &pluginsdk.WebhookRequest{
		WebhookKey: webhookKeySessionCost, Method: "DELETE", Query: "active=x",
	})
	require.NoError(t, err)
	require.Equal(t, int32(405), resp.Status)
}

func webhookPost(key, query string) *pluginsdk.WebhookRequest {
	return &pluginsdk.WebhookRequest{WebhookKey: key, Method: "POST", Query: query}
}
