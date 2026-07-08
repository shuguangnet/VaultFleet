package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

const browseRequestTimeout = 15 * time.Second
const dirSizeRequestTimeout = 30 * time.Second

type BrowseHandler struct {
	DB  *db.Database
	Hub BrowseHub

	timeout        time.Duration
	dirSizeTimeout time.Duration
	sendAndWait    func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error)
}

type BrowseHub interface {
	IsOnline(agentID string) bool
	SendAndWait(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error)
}

type browseAgentRequest struct {
	Path  string `json:"path" binding:"required"`
	Depth int    `json:"depth"`
}

type dirSizeRequest struct {
	Path string `json:"path" binding:"required"`
}

type databaseDiscoveryRequest struct {
	Source protocol.DatabaseBackupSource `json:"source" binding:"required"`
}

func NewBrowseHandler(database *db.Database, hub BrowseHub) *BrowseHandler {
	handler := &BrowseHandler{
		DB:      database,
		Hub:     hub,
		timeout: browseRequestTimeout,
	}
	handler.sendAndWait = func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
		return handler.Hub.SendAndWait(agentID, msg, timeout)
	}
	handler.dirSizeTimeout = dirSizeRequestTimeout
	return handler
}

func RegisterBrowseRoutes(rg *gin.RouterGroup, h *BrowseHandler) {
	rg.POST("/agents/:id/browse", h.BrowseAgent)
	rg.POST("/agents/:id/dir-size", h.DirSize)
	rg.POST("/agents/:id/docker/discover", h.DiscoverDocker)
	rg.POST("/agents/:id/database/discover", h.DiscoverDatabases)
}

func (h *BrowseHandler) BrowseAgent(c *gin.Context) {
	agentID := c.Param("id")
	if !h.agentExists(c, agentID) {
		return
	}
	if h.Hub == nil || !h.Hub.IsOnline(agentID) {
		writeErrorResponse(c, http.StatusBadGateway, "agent offline")
		return
	}

	var request browseAgentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, "invalid request")
		return
	}
	if request.Depth <= 0 || request.Depth > 3 {
		request.Depth = 2
	}

	msg, err := protocol.NewMessage(protocol.TypeDirBrowseReq, protocol.DirBrowseReqPayload{
		Path:  request.Path,
		Depth: request.Depth,
	})
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "encode browse request")
		return
	}

	wait := h.sendAndWait
	if wait == nil && h.Hub != nil {
		wait = h.Hub.SendAndWait
	}
	if wait == nil {
		writeErrorResponse(c, http.StatusBadGateway, "agent offline")
		return
	}
	respCh, err := wait(agentID, *msg, h.timeout)
	if err != nil {
		writeErrorResponse(c, http.StatusBadGateway, "agent offline")
		return
	}

	select {
	case resp, ok := <-respCh:
		if !ok {
			writeErrorResponse(c, http.StatusGatewayTimeout, "timeout waiting for agent response")
			return
		}
		payload, err := protocol.ParsePayload[protocol.DirBrowseRespPayload](&resp)
		if err != nil {
			writeErrorResponse(c, http.StatusBadGateway, "invalid agent response")
			return
		}
		writeDataResponse(c, http.StatusOK, payload)
	case <-c.Request.Context().Done():
		writeErrorResponse(c, http.StatusGatewayTimeout, "request cancelled")
	}
}

func (h *BrowseHandler) DirSize(c *gin.Context) {
	agentID := c.Param("id")
	if !h.agentExists(c, agentID) {
		return
	}
	if h.Hub == nil || !h.Hub.IsOnline(agentID) {
		writeErrorResponse(c, http.StatusBadGateway, "agent offline")
		return
	}

	var request dirSizeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, "invalid request")
		return
	}

	msg, err := protocol.NewMessage(protocol.TypeDirSizeReq, protocol.DirSizeReqPayload{
		Path: request.Path,
	})
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "encode dir size request")
		return
	}

	timeout := h.dirSizeTimeout
	if timeout == 0 {
		timeout = dirSizeRequestTimeout
	}

	wait := h.sendAndWait
	if wait == nil && h.Hub != nil {
		wait = h.Hub.SendAndWait
	}
	if wait == nil {
		writeErrorResponse(c, http.StatusBadGateway, "agent offline")
		return
	}
	respCh, err := wait(agentID, *msg, timeout)
	if err != nil {
		writeErrorResponse(c, http.StatusBadGateway, "agent offline")
		return
	}

	select {
	case resp, ok := <-respCh:
		if !ok {
			writeErrorResponse(c, http.StatusGatewayTimeout, "timeout waiting for agent response")
			return
		}
		payload, err := protocol.ParsePayload[protocol.DirSizeRespPayload](&resp)
		if err != nil {
			writeErrorResponse(c, http.StatusBadGateway, "invalid agent response")
			return
		}
		writeDataResponse(c, http.StatusOK, payload)
	case <-c.Request.Context().Done():
		writeErrorResponse(c, http.StatusGatewayTimeout, "request cancelled")
	}
}

func (h *BrowseHandler) DiscoverDocker(c *gin.Context) {
	agentID := c.Param("id")
	if !h.agentExists(c, agentID) {
		return
	}
	supported, err := agentHasCapability(h.DB, agentID, protocol.CapabilityDockerWorkloadBackups)
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	if !supported {
		writeErrorResponse(c, http.StatusBadRequest, "agent does not support Docker workload backups")
		return
	}
	if h.Hub == nil || !h.Hub.IsOnline(agentID) {
		writeErrorResponse(c, http.StatusBadGateway, "agent offline")
		return
	}

	msg, err := protocol.NewMessage(protocol.TypeDockerDiscoveryReq, protocol.DockerDiscoveryReqPayload{})
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "encode docker discovery request")
		return
	}

	wait := h.sendAndWait
	if wait == nil && h.Hub != nil {
		wait = h.Hub.SendAndWait
	}
	if wait == nil {
		writeErrorResponse(c, http.StatusBadGateway, "agent offline")
		return
	}
	respCh, err := wait(agentID, *msg, h.timeout)
	if err != nil {
		writeErrorResponse(c, http.StatusBadGateway, "agent offline")
		return
	}

	select {
	case resp, ok := <-respCh:
		if !ok {
			writeErrorResponse(c, http.StatusGatewayTimeout, "timeout waiting for agent response")
			return
		}
		payload, err := protocol.ParsePayload[protocol.DockerDiscoveryRespPayload](&resp)
		if err != nil {
			writeErrorResponse(c, http.StatusBadGateway, "invalid agent response")
			return
		}
		writeDataResponse(c, http.StatusOK, payload)
	case <-c.Request.Context().Done():
		writeErrorResponse(c, http.StatusGatewayTimeout, "request cancelled")
	}
}

func (h *BrowseHandler) DiscoverDatabases(c *gin.Context) {
	agentID := c.Param("id")
	if !h.agentExists(c, agentID) {
		return
	}
	supported, err := agentHasCapability(h.DB, agentID, protocol.CapabilityDatabaseBackups)
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	if !supported {
		writeErrorResponse(c, http.StatusBadRequest, "agent does not support database backups")
		return
	}
	if h.Hub == nil || !h.Hub.IsOnline(agentID) {
		writeErrorResponse(c, http.StatusBadGateway, "agent offline")
		return
	}

	var request databaseDiscoveryRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, "invalid request")
		return
	}
	source, ok := normalizeDatabaseDiscoverySource(c, request.Source)
	if !ok {
		return
	}

	msg, err := protocol.NewMessage(protocol.TypeDatabaseDiscoveryReq, protocol.DatabaseDiscoveryReqPayload{
		Source: source,
	})
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "encode database discovery request")
		return
	}

	wait := h.sendAndWait
	if wait == nil && h.Hub != nil {
		wait = h.Hub.SendAndWait
	}
	if wait == nil {
		writeErrorResponse(c, http.StatusBadGateway, "agent offline")
		return
	}
	respCh, err := wait(agentID, *msg, h.timeout)
	if err != nil {
		writeErrorResponse(c, http.StatusBadGateway, "agent offline")
		return
	}

	select {
	case resp, ok := <-respCh:
		if !ok {
			writeErrorResponse(c, http.StatusGatewayTimeout, "timeout waiting for agent response")
			return
		}
		payload, err := protocol.ParsePayload[protocol.DatabaseDiscoveryRespPayload](&resp)
		if err != nil {
			writeErrorResponse(c, http.StatusBadGateway, "invalid agent response")
			return
		}
		writeDataResponse(c, http.StatusOK, payload)
	case <-c.Request.Context().Done():
		writeErrorResponse(c, http.StatusGatewayTimeout, "request cancelled")
	}
}

func normalizeDatabaseDiscoverySource(c *gin.Context, source protocol.DatabaseBackupSource) (protocol.DatabaseBackupSource, bool) {
	source.Engine = strings.ToLower(strings.TrimSpace(source.Engine))
	switch source.Engine {
	case protocol.DatabaseEnginePostgreSQL, protocol.DatabaseEngineMySQL:
	default:
		writeErrorResponse(c, http.StatusBadRequest, "database source engine must be postgresql or mysql")
		return protocol.DatabaseBackupSource{}, false
	}
	source.ExecutionMode = strings.ToLower(strings.TrimSpace(source.ExecutionMode))
	switch source.ExecutionMode {
	case protocol.DatabaseExecutionHost, protocol.DatabaseExecutionDocker:
	default:
		writeErrorResponse(c, http.StatusBadRequest, "database source execution_mode must be host or docker")
		return protocol.DatabaseBackupSource{}, false
	}
	source.Host = strings.TrimSpace(source.Host)
	source.Username = strings.TrimSpace(source.Username)
	source.Password = strings.TrimSpace(source.Password)
	source.Database = ""
	source.AllDatabases = true
	source.OutputName = ""
	source.ConnectionName = strings.TrimSpace(source.ConnectionName)
	source.ExtraArgs = normalizePolicyPathList(source.ExtraArgs)
	if source.Username == "" {
		writeErrorResponse(c, http.StatusBadRequest, "database source username is required")
		return protocol.DatabaseBackupSource{}, false
	}
	if source.Port < 0 || source.Port > 65535 {
		writeErrorResponse(c, http.StatusBadRequest, "database source port must be between 0 and 65535")
		return protocol.DatabaseBackupSource{}, false
	}
	if source.ExecutionMode == protocol.DatabaseExecutionDocker {
		if source.DockerContainer == nil || strings.TrimSpace(firstDatabaseDiscoveryContainerIdentity(*source.DockerContainer)) == "" {
			writeErrorResponse(c, http.StatusBadRequest, "database docker source needs a container")
			return protocol.DatabaseBackupSource{}, false
		}
		normalizeDockerContainerSource(source.DockerContainer)
	}
	if source.DumpTimeoutSeconds < 0 || source.DumpTimeoutSeconds > maxPolicyHookTimeoutSeconds {
		writeErrorResponse(c, http.StatusBadRequest, "database source dump_timeout_seconds must be between 0 and 3600")
		return protocol.DatabaseBackupSource{}, false
	}
	return source, true
}

func firstDatabaseDiscoveryContainerIdentity(container protocol.DockerContainerBackupSource) string {
	for _, value := range []string{container.ContainerID, container.Name, container.ComposeService} {
		value = strings.Trim(strings.TrimSpace(value), "/")
		if value != "" {
			return value
		}
	}
	return ""
}

func (h *BrowseHandler) agentExists(c *gin.Context, agentID string) bool {
	var agent db.Agent
	if err := h.DB.DB.First(&agent, "id = ?", agentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeErrorResponse(c, http.StatusNotFound, "agent not found")
			return false
		}
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return false
	}
	return true
}
