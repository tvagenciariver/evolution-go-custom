package chatwoot_service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/EvolutionAPI/evolution-go/pkg/chatwoot/model"
	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
	instance_repository "github.com/EvolutionAPI/evolution-go/pkg/instance/repository"
	logger_wrapper "github.com/EvolutionAPI/evolution-go/pkg/logger"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/events"
)

type ChatwootService interface {
	FindOrCreateInbox(instance *instance_model.Instance) (int, error)
	SendWhatsAppToChatwoot(instance *instance_model.Instance, evt *events.Message) error
	SendWhatsAppMessageToChatwoot(instance *instance_model.Instance, chatJID string, pushName string, messageId string, text string, isFromMe bool) error
}

type chatwootService struct {
	instanceRepo  instance_repository.InstanceRepository
	loggerWrapper *logger_wrapper.LoggerManager
	httpClient    *http.Client
}

func NewChatwootService(instanceRepo instance_repository.InstanceRepository, loggerWrapper *logger_wrapper.LoggerManager) ChatwootService {
	return &chatwootService{
		instanceRepo:  instanceRepo,
		loggerWrapper: loggerWrapper,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Helper to make authenticated HTTP requests to Chatwoot
func (s *chatwootService) request(method, cwUrl, token, path string, body interface{}) ([]byte, int, error) {
	fullUrl := fmt.Sprintf("%s%s", strings.TrimSuffix(cwUrl, "/"), path)

	var reqBody io.Reader
	if body != nil {
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reqBody = bytes.NewBuffer(jsonBytes)
	}

	req, err := http.NewRequest(method, fullUrl, reqBody)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api_access_token", token)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return respBytes, resp.StatusCode, nil
}

// FindOrCreateInbox finds or automatically creates an API inbox in Chatwoot for an instance
func (s *chatwootService) FindOrCreateInbox(instance *instance_model.Instance) (int, error) {
	if instance.ChatwootInboxId > 0 {
		return instance.ChatwootInboxId, nil
	}

	// 1. Get list of inboxes to check if it already exists
	path := fmt.Sprintf("/api/v1/accounts/%s/inboxes", instance.ChatwootAccountId)
	resp, code, err := s.request("GET", instance.ChatwootUrl, instance.ChatwootToken, path, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to list inboxes: %v (code: %d)", err, code)
	}

	var inboxes []chatwoot_model.ChatwootInbox
	if err := json.Unmarshal(resp, &inboxes); err == nil {
		for _, ib := range inboxes {
			if ib.ChannelType == "Channel::Api" && ib.Name == fmt.Sprintf("WhatsApp - %s", instance.Name) {
				s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Found existing Chatwoot Inbox ID: %d", instance.Id, ib.Id)
				return ib.Id, nil
			}
		}
	}

	// 2. Create the API inbox if it does not exist
	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Creating new Chatwoot Inbox...", instance.Id)
	body := map[string]interface{}{
		"name":         fmt.Sprintf("WhatsApp - %s", instance.Name),
		"channel_type": "api",
	}

	resp, code, err = s.request("POST", instance.ChatwootUrl, instance.ChatwootToken, path, body)
	if err != nil {
		return 0, fmt.Errorf("failed to create inbox: %v (code: %d)", err, code)
	}

	var newInbox chatwoot_model.ChatwootInbox
	if err := json.Unmarshal(resp, &newInbox); err != nil {
		return 0, fmt.Errorf("failed to unmarshal created inbox: %v (response: %s)", err, string(resp))
	}

	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Created Chatwoot Inbox successfully with ID: %d", instance.Id, newInbox.Id)
	return newInbox.Id, nil
}

// FindOrCreateContact finds or creates a Chatwoot contact for a JID/phone number
func (s *chatwootService) FindOrCreateContact(instance *instance_model.Instance, phone string, name string) (int, error) {
	// Format phone number to E164 if needed (+ prefix)
	formattedPhone := phone
	if !strings.HasPrefix(phone, "+") {
		formattedPhone = "+" + phone
	}

	// 1. Search Contact
	searchPath := fmt.Sprintf("/api/v1/accounts/%s/contacts/search?q=%s", instance.ChatwootAccountId, url.QueryEscape(formattedPhone))
	resp, code, err := s.request("GET", instance.ChatwootUrl, instance.ChatwootToken, searchPath, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to search contact: %v (code: %d)", err, code)
	}

	var searchResult struct {
		Payload []chatwoot_model.ChatwootContact `json:"payload"`
	}
	if err := json.Unmarshal(resp, &searchResult); err == nil && len(searchResult.Payload) > 0 {
		return searchResult.Payload[0].Id, nil
	}

	// 2. Create Contact if not found
	createPath := fmt.Sprintf("/api/v1/accounts/%s/contacts", instance.ChatwootAccountId)
	body := map[string]interface{}{
		"name":         name,
		"phone_number": formattedPhone,
		"custom_attributes": map[string]string{
			"jid": phone + "@s.whatsapp.net",
		},
	}

	resp, code, err = s.request("POST", instance.ChatwootUrl, instance.ChatwootToken, createPath, body)
	if err != nil {
		return 0, fmt.Errorf("failed to create contact: %v (code: %d)", err, code)
	}

	var createResult struct {
		Payload struct {
			Contact chatwoot_model.ChatwootContact `json:"contact"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(resp, &createResult); err != nil {
		return 0, fmt.Errorf("failed to unmarshal created contact: %v (response: %s)", err, string(resp))
	}

	return createResult.Payload.Contact.Id, nil
}

// FindOrCreateConversation finds an active conversation or creates one
func (s *chatwootService) FindOrCreateConversation(instance *instance_model.Instance, contactId int, inboxId int) (int, error) {
	// 1. Get contact's active conversations
	path := fmt.Sprintf("/api/v1/accounts/%s/contacts/%d/conversations", instance.ChatwootAccountId, contactId)
	resp, code, err := s.request("GET", instance.ChatwootUrl, instance.ChatwootToken, path, nil)
	if err == nil {
		var conversations []chatwoot_model.ChatwootConversation
		if err := json.Unmarshal(resp, &conversations); err == nil {
			for _, conv := range conversations {
				if conv.InboxId == inboxId {
					// Reopen if resolved/closed and setting is enabled
					if (conv.Status == "resolved" || conv.Status == "snoozed") && instance.ChatwootReopenChat {
						s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Reopening resolved conversation %d...", instance.Id, conv.Id)
						togglePath := fmt.Sprintf("/api/v1/accounts/%s/conversations/%d/toggle_status", instance.ChatwootAccountId, conv.Id)
						s.request("POST", instance.ChatwootUrl, instance.ChatwootToken, togglePath, map[string]string{"status": "open"})
					}
					return conv.Id, nil
				}
			}
		}
	}

	// 2. Create new conversation if none exists
	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Creating new conversation in Chatwoot...", instance.Id)
	createPath := fmt.Sprintf("/api/v1/accounts/%s/conversations", instance.ChatwootAccountId)
	body := map[string]interface{}{
		"source_id":  fmt.Sprintf("whatsapp-%d-%d", contactId, time.Now().Unix()),
		"inbox_id":   inboxId,
		"contact_id": contactId,
		"status":     "open",
	}

	resp, code, err = s.request("POST", instance.ChatwootUrl, instance.ChatwootToken, createPath, body)
	if err != nil {
		return 0, fmt.Errorf("failed to create conversation: %v (code: %d)", err, code)
	}

	var newConv chatwoot_model.ChatwootConversation
	if err := json.Unmarshal(resp, &newConv); err != nil {
		return 0, fmt.Errorf("failed to unmarshal conversation: %v (response: %s)", err, string(resp))
	}

	return newConv.Id, nil
}

// PostMessageToChatwoot sends a message to Chatwoot conversation
func (s *chatwootService) PostMessageToChatwoot(instance *instance_model.Instance, conversationId int, text string, isFromMe bool) error {
	path := fmt.Sprintf("/api/v1/accounts/%s/conversations/%d/messages", instance.ChatwootAccountId, conversationId)

	msgType := "incoming"
	if isFromMe {
		msgType = "outgoing"
	}

	body := map[string]interface{}{
		"content":      text,
		"message_type": msgType,
		"private":      false,
	}

	_, code, err := s.request("POST", instance.ChatwootUrl, instance.ChatwootToken, path, body)
	if err != nil {
		return fmt.Errorf("failed to post message: %v (code: %d)", err, code)
	}

	return nil
}

// SendWhatsAppToChatwoot formats WhatsApp event message and forwards it to Chatwoot
func (s *chatwootService) SendWhatsAppToChatwoot(instance *instance_model.Instance, evt *events.Message) error {
	// Only proceed if Chatwoot is configured and enabled
	if !instance.ChatwootEnabled || instance.ChatwootUrl == "" || instance.ChatwootToken == "" || instance.ChatwootAccountId == "" {
		return nil
	}

	// Ignore group status updates
	if strings.Contains(evt.Info.Chat.String(), "@g.us") && instance.IgnoreGroups {
		return nil
	}

	// Extract clean sender/chat JID
	chatJID := evt.Info.Chat.User
	pushName := evt.Info.PushName
	if pushName == "" {
		pushName = chatJID
	}

	// Extract message text content safely
	text := s.extractMessageText(evt.Message)
	if text == "" {
		return nil // Ignore empty or unsupported protocols
	}

	return s.SendWhatsAppMessageToChatwoot(instance, chatJID, pushName, evt.Info.ID, text, evt.Info.IsFromMe)
}

// SendWhatsAppMessageToChatwoot handles the core logic of forwarding a message to Chatwoot
func (s *chatwootService) SendWhatsAppMessageToChatwoot(instance *instance_model.Instance, chatJID string, pushName string, messageId string, text string, isFromMe bool) error {
	defer func() {
		if r := recover(); r != nil {
			s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Panic in SendWhatsAppMessageToChatwoot: %v", instance.Id, r)
		}
	}()

	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Forwarding WhatsApp message to Chatwoot (ID: %s, JID: %s, IsFromMe: %v)", instance.Id, messageId, chatJID, isFromMe)

	// 1. Ensure Inbox exists
	inboxId, err := s.FindOrCreateInbox(instance)
	if err != nil {
		return fmt.Errorf("inbox error: %v", err)
	}

	// Save inbox ID to instance if changed
	if instance.ChatwootInboxId != inboxId {
		instance.ChatwootInboxId = inboxId
		_ = s.instanceRepo.Update(instance)
	}

	// 2. Find or Create Contact
	contactId, err := s.FindOrCreateContact(instance, chatJID, pushName)
	if err != nil {
		return fmt.Errorf("contact error: %v", err)
	}

	// 3. Find or Create Conversation
	conversationId, err := s.FindOrCreateConversation(instance, contactId, inboxId)
	if err != nil {
		return fmt.Errorf("conversation error: %v", err)
	}

	// 4. Post Message
	err = s.PostMessageToChatwoot(instance, conversationId, text, isFromMe)
	if err != nil {
		return fmt.Errorf("post message error: %v", err)
	}

	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] WhatsApp message successfully forwarded to Chatwoot Conversation: %d", instance.Id, conversationId)
	return nil
}

// Helper to extract readable string content from any waE2E.Message type
func (s *chatwootService) extractMessageText(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}

	if msg.Conversation != nil {
		return *msg.Conversation
	}
	if msg.ExtendedTextMessage != nil && msg.ExtendedTextMessage.Text != nil {
		return *msg.ExtendedTextMessage.Text
	}
	if msg.ImageMessage != nil {
		caption := ""
		if msg.ImageMessage.Caption != nil {
			caption = " - " + *msg.ImageMessage.Caption
		}
		return fmt.Sprintf("[📷 Imagem%s]", caption)
	}
	if msg.AudioMessage != nil {
		return "[🎵 Áudio]"
	}
	if msg.VideoMessage != nil {
		caption := ""
		if msg.VideoMessage.Caption != nil {
			caption = " - " + *msg.VideoMessage.Caption
		}
		return fmt.Sprintf("[🎥 Vídeo%s]", caption)
	}
	if msg.DocumentMessage != nil {
		filename := "Documento"
		if msg.DocumentMessage.FileName != nil {
			filename = *msg.DocumentMessage.FileName
		}
		return fmt.Sprintf("[📄 Documento: %s]", filename)
	}
	if msg.LocationMessage != nil {
		return fmt.Sprintf("[📍 Localização: Lat=%f, Long=%f]", *msg.LocationMessage.DegreesLatitude, *msg.LocationMessage.DegreesLongitude)
	}
	if msg.StickerMessage != nil {
		return "[Sticker/Figurinha]"
	}
	if msg.ContactMessage != nil {
		return fmt.Sprintf("[👥 Contato: %s]", *msg.ContactMessage.DisplayName)
	}
	if msg.ButtonsResponseMessage != nil {
		return fmt.Sprintf("[🔘 Resposta de Botão: %s]", *msg.ButtonsResponseMessage.SelectedButtonID)
	}
	if msg.TemplateButtonReplyMessage != nil {
		return fmt.Sprintf("[🔘 Resposta de Botão: %s]", *msg.TemplateButtonReplyMessage.SelectedId)
	}
	if msg.ListResponseMessage != nil {
		return fmt.Sprintf("[📝 Item Selecionado da Lista: %s]", *msg.ListResponseMessage.Title)
	}

	return ""
}
