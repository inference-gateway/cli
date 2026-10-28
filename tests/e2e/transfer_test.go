//go:build e2e

package e2e

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	uuid "github.com/google/uuid"
)

// peerCA writes a CA, and a server certificate for loopback signed by it,
// into dir, and returns the file paths plus the client TLS config that trusts
// the CA.
func peerCA(t *testing.T, dir string) (certFile, keyFile, caFile string, client *tls.Config) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "e2e ca"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	caCert, _ := x509.ParseCertificate(caDER)

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "peer"},
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	require.NoError(t, err)
	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	require.NoError(t, err)

	write := func(name, typ string, der []byte) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0600))
		return path
	}
	certFile = write("server.crt", "CERTIFICATE", serverDER)
	keyFile = write("server.key", "EC PRIVATE KEY", serverKeyDER)
	caFile = write("ca.crt", "CERTIFICATE", caDER)
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	return certFile, keyFile, caFile, &tls.Config{RootCAs: pool, ServerName: "localhost", MinVersion: tls.VersionTLS13}
}

// gitProject makes a project dir a git checkout with one commit, which a
// transfer needs to bundle.
func gitProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("transfer me\n"), 0644))
	for _, args := range [][]string{
		{"init", "--quiet", "-b", "main"}, {"config", "user.email", "e2e@example.com"}, {"config", "user.name", "e2e"},
		{"config", "commit.gpgsign", "false"}, {"add", "."}, {"commit", "--quiet", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	return dir
}

// peers starts daemon A, which registers daemon B as an executor of kind
// daemon, and daemon B, which serves its binding over TLS as executor "b".
func peers(t *testing.T) (a, b *daemonProc, workspaceRoot string) {
	t.Helper()
	homeA, homeB := t.TempDir(), t.TempDir()
	certFile, keyFile, caFile, clientTLS := peerCA(t, homeB)
	workspaceRoot = filepath.Join(homeB, "workspaces")
	b = startDaemonWithTLS(t, homeB, clientTLS,
		"INFER_DAEMON_EXECUTOR=b",
		"INFER_DAEMON_WORKSPACE_ROOT="+workspaceRoot,
		"INFER_DAEMON_BINDING_TLS_MODE=files",
		"INFER_DAEMON_BINDING_TLS_CERT_FILE="+certFile,
		"INFER_DAEMON_BINDING_TLS_KEY_FILE="+keyFile,
	)
	daemonYAML := fmt.Sprintf(`executor: a
executors:
  - name: b
    kind: daemon
    address: 127.0.0.1:%d
    token: %s
    lease_ttl: 1h
    tls:
      ca_file: %s
`, b.port, daemonToken, caFile)
	require.NoError(t, os.MkdirAll(filepath.Join(homeA, ".infer"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(homeA, ".infer", "daemon.yaml"), []byte(daemonYAML), 0644))
	a = startDaemon(t, homeA)
	return a, b, workspaceRoot
}

func startDaemonWithTLS(t *testing.T, home string, clientTLS *tls.Config, extraEnv ...string) *daemonProc {
	t.Helper()
	d := startDaemon(t, home, extraEnv...)
	d.tls = clientTLS
	return d
}

// transfer asks daemon A to move the thread and collects the progress stages
// through ready or failed.
func transfer(c *daemonClient, project, threadID, target, transferID string) []string {
	c.t.Helper()
	c.send(map[string]any{"type": "transfer_session", "transfer_id": transferID, "project_dir": project, "id": threadID, "target": target})
	var stages []string
	for {
		frame := c.next("transfer_progress")
		stage, _ := frame["stage"].(string)
		stages = append(stages, stage)
		if stage == "ready" || stage == "failed" {
			if stage == "failed" {
				c.t.Logf("transfer failed: %v", frame["message"])
			}
			return stages
		}
	}
}

func TestTransferMovesASessionToAnotherDaemon(t *testing.T) {
	a, b, workspaceRoot := peers(t)
	project := gitProject(t)
	desktop := dialDaemon(t, a, "desktop")
	threadID := openThread(desktop, project, "auto")
	require.NoError(t, os.WriteFile(filepath.Join(project, "notes.txt"), []byte("uncommitted\n"), 0644))

	transferID := uuid.NewString()
	stages := transfer(desktop, project, threadID, "b", transferID)
	require.Equal(t, []string{"provisioning", "checkpointing", "restoring", "ready"}, stages, "daemon A output:\n%s\ndaemon B output:\n%s", a.output, b.output)
	moved := desktop.next("session_moved")
	require.Equal(t, "b", moved["executor"])
	workspace, _ := moved["workspace"].(string)
	require.True(t, filepath.HasPrefix(workspace, workspaceRoot), "workspace %q is not under %q", workspace, workspaceRoot)
	require.FileExists(t, filepath.Join(workspace, "notes.txt"), "the uncommitted file must travel with the workspace")

	require.Equal(t, []string{"ready"}, transfer(desktop, project, threadID, "b", transferID), "a retry of the same transfer is a no-op")

	relay := dialDaemon(t, a, "desktop")
	relay.send(map[string]any{"type": "resume_conversation", "project_dir": project, "id": threadID})
	require.True(t, snapshotContains(relay.next("MESSAGES_SNAPSHOT"), "Hello! How can I help?"), "daemon A must relay the moved session")
	relay.say("say hello again")
	events := relay.run()
	require.Equal(t, "RUN_FINISHED", events[len(events)-1]["type"], "events: %v\ndaemon A:\n%s\ndaemon B:\n%s", events, a.output, b.output)

	direct := dialDaemon(t, b, "desktop")
	direct.send(map[string]any{"type": "resume_conversation", "project_dir": workspace, "id": threadID})
	snapshot := direct.next("MESSAGES_SNAPSHOT")
	require.True(t, snapshotContains(snapshot, "Hello! How can I help?"), "daemon B must hold the conversation")
	messages, _ := snapshot["messages"].([]any)
	require.GreaterOrEqual(t, len(messages), 4, "the turn run through the relay must be on daemon B: %v", snapshot)

	require.NoError(t, a.stop(t))
	direct.say("say hello")
	events = direct.run()
	require.Equal(t, "RUN_FINISHED", events[len(events)-1]["type"], "daemon B must keep serving after daemon A is gone: %v", events)
}

func TestTransferFailuresLeaveTheSessionAtHome(t *testing.T) {
	a, _, _ := peers(t)
	project := gitProject(t)
	desktop := dialDaemon(t, a, "desktop")
	threadID := openThread(desktop, project, "auto")

	stages := transfer(desktop, project, threadID, "nowhere", uuid.NewString())
	require.Equal(t, []string{"failed"}, stages)

	desktop.say("say hello again")
	events := desktop.run()
	require.Equal(t, "RUN_FINISHED", events[len(events)-1]["type"], "the session must still run at home: %v", events)
}

func TestListExecutorsReportsReachability(t *testing.T) {
	home := t.TempDir()
	deadPort := freePort(t)
	_, _, caFile, _ := peerCA(t, home)
	daemonYAML := fmt.Sprintf(`executor: a
executors:
  - name: ghost
    kind: daemon
    address: 127.0.0.1:%d
    token: x
    tls:
      ca_file: %s
`, deadPort, caFile)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".infer"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".infer", "daemon.yaml"), []byte(daemonYAML), 0644))
	d := startDaemon(t, home)
	client := dialDaemon(t, d, "desktop")

	client.send(map[string]any{"type": "list_executors"})
	executors, _ := client.next("executors")["executors"].([]any)
	require.Len(t, executors, 2, "daemon output:\n%s", d.output)
	local := executors[0].(map[string]any)
	require.Equal(t, "a", local["name"])
	require.Equal(t, true, local["reachable"])
	ghost := executors[1].(map[string]any)
	require.Equal(t, "ghost", ghost["name"])
	require.Equal(t, false, ghost["reachable"])
	require.NotEmpty(t, ghost["error"], "an unreachable executor is reported, not hidden")
}
