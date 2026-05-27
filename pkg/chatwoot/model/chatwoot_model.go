package chatwoot_model

// ChatwootSetRequest represents the body of the POST /chatwoot/set/:instance endpoint
type ChatwootSetRequest struct {
	Enabled       bool   `json:"enabled"`
	AccountId     string `json:"accountId"`
	Token         string `json:"token"`
	Url           string `json:"url"`
	SignMsg       bool   `json:"signMsg"`
	ReopenChat    bool   `json:"reopenChat"`
	AutoCreate    bool   `json:"autoCreate"`
	ImportHistory bool   `json:"importHistory"`
	InboxId       int    `json:"inboxId,omitempty"`
}

// ChatwootResponse represents the standard response showing the integration state
type ChatwootResponse struct {
	Enabled       bool   `json:"enabled"`
	AccountId     string `json:"accountId"`
	Token         string `json:"token"`
	Url           string `json:"url"`
	SignMsg       bool   `json:"signMsg"`
	ReopenChat    bool   `json:"reopenChat"`
	AutoCreate    bool   `json:"autoCreate"`
	ImportHistory bool   `json:"importHistory"`
	InboxId       int    `json:"inboxId"`
}

// ChatwootInbox represents an Inbox object in Chatwoot API
type ChatwootInbox struct {
	Id           int    `json:"id"`
	Name         string `json:"name"`
	ChannelType  string `json:"channel_type"`
}

// ChatwootContact represents a Contact object in Chatwoot API
type ChatwootContact struct {
	Id               int               `json:"id"`
	Name             string            `json:"name"`
	PhoneNumber      string            `json:"phone_number"`
	CustomAttributes map[string]string `json:"custom_attributes"`
}

// ChatwootConversation represents a Conversation object in Chatwoot API
type ChatwootConversation struct {
	Id        int    `json:"id"`
	InboxId   int    `json:"inbox_id"`
	ContactId int    `json:"contact_id"`
	Status    string `json:"status"`
}

// ChatwootMessage represents a Message object in Chatwoot API
type ChatwootMessage struct {
	Id          int    `json:"id"`
	Content     string `json:"content"`
	MessageType string `json:"message_type"`
}

// ChatwootWebhookPayload represents the payload sent by Chatwoot on webhook trigger
type ChatwootWebhookPayload struct {
	Event        string `json:"event"`
	MessageType  string `json:"message_type"`
	Private      bool   `json:"private"`
	Content      string `json:"content"`
	Contact      struct {
		Id          int    `json:"id"`
		Name        string `json:"name"`
		PhoneNumber string `json:"phone_number"`
	} `json:"contact"`
	Conversation struct {
		Id      int `json:"id"`
		InboxId int `json:"inbox_id"`
		Contact struct {
			Id          int    `json:"id"`
			Name        string `json:"name"`
			PhoneNumber string `json:"phone_number"`
		} `json:"contact"`
	} `json:"conversation"`
	Sender struct {
		Id   int    `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"` // "user" / "agent"
	} `json:"sender"`
}
