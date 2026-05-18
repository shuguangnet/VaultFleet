package agent

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/agent/policy"
	"vaultfleet/pkg/protocol"
)

func TestHandlerPolicyPushSavesPolicy(t *testing.T) {
	store := policy.NewStore(t.TempDir())
	handler := NewHandler(HandlerConfig{PolicyStore: store})
	msg, err := protocol.NewMessage(protocol.TypePolicyPush, protocol.PolicyPushPayload{
		AgentID:    "agent-1",
		BackupDirs: []string{"/srv"},
		Schedule:   "0 4 * * *",
	})
	require.NoError(t, err)

	handler.Handle(*msg)

	stored, err := store.LoadPolicy()
	require.NoError(t, err)
	assert.Equal(t, "agent-1", stored.AgentID)
	assert.Equal(t, []string{"/srv"}, stored.BackupDirs)
	assert.Equal(t, "0 4 * * *", stored.Schedule)
}

func TestHandlerDirBrowseReqSendsResponseWithSameID(t *testing.T) {
	var browsedPath string
	var browsedDepth int
	sent := make(chan protocol.Message, 1)
	handler := NewHandler(HandlerConfig{
		PolicyStore: policy.NewStore(""),
		BrowseFunc: func(fsRoot string, scanPath string, maxDepth int) ([]protocol.DirEntry, error) {
			assert.Equal(t, "/", fsRoot)
			browsedPath = scanPath
			browsedDepth = maxDepth
			return []protocol.DirEntry{{Path: "/etc", Type: "dir", Size: 4096}}, nil
		},
		SendFunc: func(msg protocol.Message) error {
			sent <- msg
			return nil
		},
	})
	req, err := protocol.NewMessage(protocol.TypeDirBrowseReq, protocol.DirBrowseReqPayload{Path: "/etc", Depth: 3})
	require.NoError(t, err)

	handler.Handle(*req)

	assert.Equal(t, "/etc", browsedPath)
	assert.Equal(t, 3, browsedDepth)
	resp := <-sent
	assert.Equal(t, protocol.TypeDirBrowseResp, resp.Type)
	assert.Equal(t, req.ID, resp.ID)
	payload, err := protocol.ParsePayload[protocol.DirBrowseRespPayload](&resp)
	require.NoError(t, err)
	assert.Equal(t, "/etc", payload.Path)
	assert.Empty(t, payload.Error)
	assert.Equal(t, []protocol.DirEntry{{Path: "/etc", Type: "dir", Size: 4096}}, payload.Entries)
}

func TestHandlerDirBrowseReqNormalizesInvalidDepth(t *testing.T) {
	var browsedDepth int
	handler := NewHandler(HandlerConfig{
		PolicyStore: policy.NewStore(""),
		BrowseFunc: func(_ string, _ string, maxDepth int) ([]protocol.DirEntry, error) {
			browsedDepth = maxDepth
			return nil, nil
		},
		SendFunc: func(protocol.Message) error {
			return nil
		},
	})
	rawPayload, err := json.Marshal(protocol.DirBrowseReqPayload{Path: "/var", Depth: 99})
	require.NoError(t, err)

	handler.Handle(protocol.Message{Type: protocol.TypeDirBrowseReq, ID: "browse-1", Payload: rawPayload})

	assert.Equal(t, 2, browsedDepth)
}

func TestHandlerDirBrowseReqSendsErrorPayload(t *testing.T) {
	sent := make(chan protocol.Message, 1)
	handler := NewHandler(HandlerConfig{
		PolicyStore: policy.NewStore(""),
		BrowseFunc: func(string, string, int) ([]protocol.DirEntry, error) {
			return nil, errors.New("permission denied")
		},
		SendFunc: func(msg protocol.Message) error {
			sent <- msg
			return nil
		},
	})
	req, err := protocol.NewMessage(protocol.TypeDirBrowseReq, protocol.DirBrowseReqPayload{Path: "/root", Depth: 2})
	require.NoError(t, err)

	handler.Handle(*req)

	resp := <-sent
	payload, err := protocol.ParsePayload[protocol.DirBrowseRespPayload](&resp)
	require.NoError(t, err)
	assert.Equal(t, "/root", payload.Path)
	assert.Equal(t, "permission denied", payload.Error)
	assert.Nil(t, payload.Entries)
}
