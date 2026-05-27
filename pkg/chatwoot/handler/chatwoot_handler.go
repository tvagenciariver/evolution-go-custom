package chatwoot_handler

import (
	"fmt"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	chatwoot_model "github.com/EvolutionAPI/evolution-go/pkg/chatwoot/model"
	chatwoot_service "github.com/EvolutionAPI/evolution-go/pkg/chatwoot/service"
	instance_repository "github.com/EvolutionAPI/evolution-go/pkg/instance/repository"
	logger_wrapper "github.com/EvolutionAPI/evolution-go/pkg/logger"
	send_service "github.com/EvolutionAPI/evolution-go/pkg/sendMessage/service"
)

type ChatwootHandler interface {
	SetConfiguration(c *gin.Context)
	GetConfiguration(c *gin.Context)
	DeleteConfiguration(c *gin.Context)
	HandleWebhook(c *gin.Context)
}

type chatwootHandler struct {
	cwService     chatwoot_service.ChatwootService
	instanceRepo  instance_repository.InstanceRepository
	sendService   send_service.SendService
	loggerWrapper *logger_wrapper.LoggerManager
}

func NewChatwootHandler(cwService chatwoot_service.ChatwootService, instanceRepo instance_repository.InstanceRepository, sendService send_service.SendService, loggerWrapper *logger_wrapper.LoggerManager) ChatwootHandler {
	return &chatwootHandler{
		cwService:     cwService,
		instanceRepo:  instanceRepo,
		sendService:   sendService,
		loggerWrapper: loggerWrapper,
	}
}

// POST /chatwoot/set/:instanceId
func (h *chatwootHandler) SetConfiguration(c *gin.Context) {
	instanceId := c.Param("instanceId")
	if instanceId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "instanceId param is required"})
		return
	}

	instance, err := h.instanceRepo.GetInstanceByID(instanceId)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("instance not found: %v", err)})
		return
	}

	var req chatwoot_model.ChatwootSetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid request body: %v", err)})
		return
	}

	instance.ChatwootEnabled = req.Enabled
	instance.ChatwootAccountId = req.AccountId
	instance.ChatwootToken = req.Token
	instance.ChatwootUrl = req.Url
	instance.ChatwootSignMsg = req.SignMsg
	instance.ChatwootReopenChat = req.ReopenChat
	instance.ChatwootAutoCreate = req.AutoCreate
	instance.ChatwootImportHistory = req.ImportHistory
	if req.InboxId > 0 {
		instance.ChatwootInboxId = req.InboxId
	}

	// Try to auto-create inbox in Chatwoot if enabled
	if instance.ChatwootEnabled && instance.ChatwootAutoCreate && instance.ChatwootInboxId == 0 {
		inboxId, err := h.cwService.FindOrCreateInbox(instance)
		if err != nil {
			h.loggerWrapper.GetLogger(instanceId).LogError("[%s] Failed to automatically create Chatwoot inbox: %v", instanceId, err)
		} else {
			instance.ChatwootInboxId = inboxId
		}
	}

	if err := h.instanceRepo.Update(instance); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save configuration: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "SUCCESS",
		"message": "Chatwoot configuration saved successfully",
		"data": chatwoot_model.ChatwootResponse{
			Enabled:       instance.ChatwootEnabled,
			AccountId:     instance.ChatwootAccountId,
			Token:         instance.ChatwootToken,
			Url:           instance.ChatwootUrl,
			SignMsg:       instance.ChatwootSignMsg,
			ReopenChat:    instance.ChatwootReopenChat,
			AutoCreate:    instance.ChatwootAutoCreate,
			ImportHistory: instance.ChatwootImportHistory,
			InboxId:       instance.ChatwootInboxId,
		},
	})
}

// GET /chatwoot/find/:instanceId
func (h *chatwootHandler) GetConfiguration(c *gin.Context) {
	instanceId := c.Param("instanceId")
	if instanceId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "instanceId param is required"})
		return
	}

	instance, err := h.instanceRepo.GetInstanceByID(instanceId)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("instance not found: %v", err)})
		return
	}

	c.JSON(http.StatusOK, chatwoot_model.ChatwootResponse{
		Enabled:       instance.ChatwootEnabled,
		AccountId:     instance.ChatwootAccountId,
		Token:         instance.ChatwootToken,
		Url:           instance.ChatwootUrl,
		SignMsg:       instance.ChatwootSignMsg,
		ReopenChat:    instance.ChatwootReopenChat,
		AutoCreate:    instance.ChatwootAutoCreate,
		ImportHistory: instance.ChatwootImportHistory,
		InboxId:       instance.ChatwootInboxId,
	})
}

// POST /chatwoot/delete/:instanceId
func (h *chatwootHandler) DeleteConfiguration(c *gin.Context) {
	instanceId := c.Param("instanceId")
	if instanceId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "instanceId param is required"})
		return
	}

	instance, err := h.instanceRepo.GetInstanceByID(instanceId)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("instance not found: %v", err)})
		return
	}

	instance.ChatwootEnabled = false
	instance.ChatwootAccountId = ""
	instance.ChatwootToken = ""
	instance.ChatwootUrl = ""
	instance.ChatwootInboxId = 0

	if err := h.instanceRepo.Update(instance); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to delete configuration: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "SUCCESS",
		"message": "Chatwoot configuration disabled and cleared",
	})
}

// POST /chatwoot/webhook/:instanceId
func (h *chatwootHandler) HandleWebhook(c *gin.Context) {
	instanceId := c.Param("instanceId")
	if instanceId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "instanceId param is required"})
		return
	}

	instance, err := h.instanceRepo.GetInstanceByID(instanceId)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("instance not found: %v", err)})
		return
	}

	if !instance.ChatwootEnabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "Chatwoot integration is disabled for this instance"})
		return
	}

	var payload chatwoot_model.ChatwootWebhookPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid webhook payload: %v", err)})
		return
	}

	h.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Received webhook from Chatwoot. Event: %s, MessageType: %s, Private: %t", instanceId, payload.Event, payload.MessageType, payload.Private)

	// We only process message_created event from agents (outgoing) that are NOT private notes
	if payload.Event != "message_created" || payload.MessageType != "outgoing" || payload.Private {
		c.JSON(http.StatusOK, gin.H{"status": "IGNORED", "reason": "event is not an outgoing agent message"})
		return
	}

	// Clean contact phone number
	rawPhone := payload.Conversation.Contact.PhoneNumber
	if rawPhone == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "contact phone number is empty"})
		return
	}

	// Strip everything except digits to obtain a clean phone number
	reg := regexp.MustCompile(`[^0-9]`)
	cleanPhone := reg.ReplaceAllString(rawPhone, "")

	if cleanPhone == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "contact phone number does not contain valid digits"})
		return
	}

	// Format text message. Sign with agent's name if configured
	messageText := payload.Content
	if instance.ChatwootSignMsg && payload.Sender.Name != "" {
		messageText = fmt.Sprintf("*%s*: %s", payload.Sender.Name, payload.Content)
	}

	// Build TextStruct for sending via SendText service
	textData := &send_service.TextStruct{
		Number: cleanPhone,
		Text:   messageText,
	}

	// Send to WhatsApp
	h.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Sending Chatwoot reply to WhatsApp recipient: %s", instanceId, cleanPhone)
	_, err = h.sendService.SendText(textData, instance)
	if err != nil {
		h.loggerWrapper.GetLogger(instanceId).LogError("[%s] Failed to send WhatsApp message from Chatwoot reply: %v", instanceId, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to send WhatsApp message: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "SUCCESS",
		"message": "Message successfully delivered to WhatsApp",
	})
}
