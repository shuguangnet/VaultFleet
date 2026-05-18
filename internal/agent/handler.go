package agent

import (
	"log"

	"vaultfleet/internal/agent/filebrowse"
	"vaultfleet/internal/agent/policy"
	"vaultfleet/pkg/protocol"
)

type SendFunc func(protocol.Message) error

type BrowseFunc func(fsRoot string, scanPath string, maxDepth int) ([]protocol.DirEntry, error)

type HandlerConfig struct {
	PolicyStore *policy.Store
	SendFunc    SendFunc
	BrowseFunc  BrowseFunc
}

type Handler struct {
	policyStore *policy.Store
	send        SendFunc
	browse      BrowseFunc
}

func NewHandler(config HandlerConfig) *Handler {
	browse := config.BrowseFunc
	if browse == nil {
		browse = filebrowse.Browse
	}
	return &Handler{
		policyStore: config.PolicyStore,
		send:        config.SendFunc,
		browse:      browse,
	}
}

func (h *Handler) Handle(msg protocol.Message) {
	switch msg.Type {
	case protocol.TypePolicyPush:
		h.handlePolicyPush(msg)
	case protocol.TypeDirBrowseReq:
		h.handleDirBrowseReq(msg)
	}
}

func (h *Handler) handlePolicyPush(msg protocol.Message) {
	if h.policyStore == nil {
		return
	}

	pushedPolicy, err := protocol.ParsePayload[protocol.PolicyPushPayload](&msg)
	if err != nil {
		log.Printf("parse policy push failed: %v", err)
		return
	}
	if err := h.policyStore.SavePolicy(pushedPolicy); err != nil {
		log.Printf("save policy failed: %v", err)
	}
}

func (h *Handler) handleDirBrowseReq(msg protocol.Message) {
	req, err := protocol.ParsePayload[protocol.DirBrowseReqPayload](&msg)
	if err != nil {
		log.Printf("parse directory browse request failed: %v", err)
		return
	}

	if req.Depth <= 0 || req.Depth > 3 {
		req.Depth = 2
	}

	entries, browseErr := h.browse("/", req.Path, req.Depth)
	payload := protocol.DirBrowseRespPayload{
		Path:    req.Path,
		Entries: entries,
	}
	if browseErr != nil {
		payload.Error = browseErr.Error()
		payload.Entries = nil
	}

	resp, err := protocol.NewMessage(protocol.TypeDirBrowseResp, payload)
	if err != nil {
		log.Printf("create directory browse response failed: %v", err)
		return
	}
	resp.ID = msg.ID

	if h.send == nil {
		return
	}
	if err := h.send(*resp); err != nil {
		log.Printf("send directory browse response failed: %v", err)
	}
}
