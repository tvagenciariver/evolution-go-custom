package chatwoot_handler

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

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
	rawPhone := payload.Contact.PhoneNumber
	if rawPhone == "" {
		rawPhone = payload.Conversation.Contact.PhoneNumber
	}

	// Fallback 1: If both are empty, query Chatwoot API directly using Contact ID
	if rawPhone == "" {
		contactId := payload.Contact.Id
		if contactId == 0 {
			contactId = payload.Conversation.Contact.Id
		}

		if contactId > 0 {
			h.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Webhook payload has empty phone. Fetching contact details dynamically for ID %d...", instanceId, contactId)
			contact, err := h.cwService.GetContact(instance, contactId)
			if err == nil && contact != nil {
				// Try direct phone number field first
				rawPhone = contact.PhoneNumber
				
				// Try custom_attributes["jid"] next (which stores the WhatsApp JID)
				if rawPhone == "" && contact.CustomAttributes != nil {
					rawPhone = contact.CustomAttributes["jid"]
				}
				
				// Try to extract phone number from contact name returned by API
				if rawPhone == "" && contact.Name != "" {
					cleanName := contact.Name
					cleanName = strings.ReplaceAll(cleanName, "@s.whatsapp.net", "")
					cleanName = strings.ReplaceAll(cleanName, "@c.us", "")
					
					reg := regexp.MustCompile(`[^0-9]`)
					digits := reg.ReplaceAllString(cleanName, "")
					
					if len(digits) >= 8 && len(digits) <= 15 {
						rawPhone = digits
						h.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Extracted phone from fetched contact name '%s': '%s'", instanceId, contact.Name, rawPhone)
					}
				}
				
				h.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Successfully retrieved contact phone from API: '%s'", instanceId, rawPhone)
			} else {
				h.loggerWrapper.GetLogger(instanceId).LogError("[%s] Failed to fetch contact %d details from Chatwoot API: %v", instanceId, contactId, err)
			}
		}
	}

	// Fallback 2: Try to extract from webhook payload's contact name
	if rawPhone == "" {
		name := payload.Contact.Name
		if name == "" {
			name = payload.Conversation.Contact.Name
		}
		
		if name != "" {
			cleanName := name
			cleanName = strings.ReplaceAll(cleanName, "@s.whatsapp.net", "")
			cleanName = strings.ReplaceAll(cleanName, "@c.us", "")
			
			reg := regexp.MustCompile(`[^0-9]`)
			digits := reg.ReplaceAllString(cleanName, "")
			
			if len(digits) >= 8 && len(digits) <= 15 {
				rawPhone = digits
				h.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Extracted phone from payload contact name '%s': '%s'", instanceId, name, rawPhone)
			}
		}
	}

	if rawPhone == "" {
		h.loggerWrapper.GetLogger(instanceId).LogError("[%s] Webhook error: Contact phone number is empty. Root phone_number='%s', Nested phone_number='%s'", instanceId, payload.Contact.PhoneNumber, payload.Conversation.Contact.PhoneNumber)
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
