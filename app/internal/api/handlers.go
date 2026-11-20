package api

import (
	"net/http"
	"time"

	"github.com/X-Ray-X/mailflow/app/internal/mail"
)

// Wire types. They are the public contract: no vendor field exists here, so no
// vendor field can be serialised.

type addressDTO struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type mailboxDTO struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Unread int    `json:"unread"`
	Total  int    `json:"total"`
}

type messageSummaryDTO struct {
	ID            string     `json:"id"`
	Subject       string     `json:"subject"`
	From          addressDTO `json:"from"`
	ReceivedAt    time.Time  `json:"receivedAt"`
	Read          bool       `json:"read"`
	Starred       bool       `json:"starred"`
	HasAttachment bool       `json:"hasAttachment"`
}

type messageDTO struct {
	ID            string       `json:"id"`
	Subject       string       `json:"subject"`
	From          addressDTO   `json:"from"`
	To            []addressDTO `json:"to"`
	Cc            []addressDTO `json:"cc"`
	ReceivedAt    time.Time    `json:"receivedAt"`
	Read          bool         `json:"read"`
	Starred       bool         `json:"starred"`
	HasAttachment bool         `json:"hasAttachment"`
	Text          string       `json:"text"`
	TextTruncated bool         `json:"textTruncated"`
}

type mailboxesResponse struct {
	Mailboxes []mailboxDTO `json:"mailboxes"`
}

type messagesResponse struct {
	Messages   []messageSummaryDTO `json:"messages"`
	NextCursor string              `json:"nextCursor,omitempty"`
	HasMore    bool                `json:"hasMore"`
}

type healthResponse struct {
	Status string `json:"status"`
}

// Health godoc
// @Summary Service health
// @Description Unauthenticated health check, independent of the mail server.
// @Tags system
// @Produce json
// @Success 200 {object} healthResponse
// @Router /healthz [get]
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if !s.requireGet(w, r) {
		return
	}
	// Deliberately independent of the mail server: a mail outage must not
	// restart the application.
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

func (s *Server) mailboxes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		s.listMailboxes(w, r)
	case http.MethodPost:
		s.createMailbox(w, r)
	default:
		writeMethodNotAllowed(w, "GET, HEAD, POST")
	}
}

// ListMailboxes godoc
// @Summary List mailboxes
// @Description All mailboxes of the caller's account.
// @Tags mailboxes
// @Security BasicAuth
// @Produce json
// @Success 200 {object} mailboxesResponse
// @Failure 401 {object} errorBody
// @Router /v1/mailboxes [get]
func (s *Server) listMailboxes(w http.ResponseWriter, r *http.Request) {
	if !s.requireGet(w, r) {
		return
	}
	list, err := s.service.Mailboxes(r.Context(), credentials(r.Context()))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	resp := mailboxesResponse{Mailboxes: make([]mailboxDTO, 0, len(list))}
	for _, m := range list {
		resp.Mailboxes = append(resp.Mailboxes, mailboxDTO{
			ID:     m.ID,
			Name:   m.Name,
			Role:   m.Role,
			Unread: m.Unread,
			Total:  m.Total,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// mailboxByID dispatches the methods the contract publishes on one mailbox:
// rename it, or delete it when it is empty.
func (s *Server) mailboxByID(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPatch:
		s.renameMailbox(w, r)
	case http.MethodDelete:
		s.deleteMailbox(w, r)
	default:
		writeMethodNotAllowed(w, "PATCH, DELETE")
	}
}

// Messages godoc
// @Summary List messages in a mailbox
// @Tags messages
// @Security BasicAuth
// @Produce json
// @Param mailboxID path string true "Mailflow mailbox identifier"
// @Param limit query int false "Page size"
// @Param cursor query string false "Next cursor from a previous page"
// @Success 200 {object} messagesResponse
// @Failure 400 {object} errorBody
// @Failure 401 {object} errorBody
// @Failure 404 {object} errorBody
// @Router /v1/mailboxes/{mailboxID}/messages [get]
func (s *Server) messages(w http.ResponseWriter, r *http.Request) {
	if !s.requireGet(w, r) {
		return
	}
	limit, err := s.pageSize(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
		return
	}

	page, err := s.service.Messages(r.Context(), credentials(r.Context()), r.PathValue("mailboxID"), mail.PageRequest{
		Limit:  limit,
		Cursor: r.URL.Query().Get("cursor"),
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}

	resp := messagesResponse{
		Messages:   make([]messageSummaryDTO, 0, len(page.Messages)),
		NextCursor: page.NextCursor,
		HasMore:    page.HasMore,
	}
	for _, m := range page.Messages {
		resp.Messages = append(resp.Messages, toMessageSummaryDTO(m))
	}
	writeJSON(w, http.StatusOK, resp)
}

// messageByID dispatches the methods the contract publishes on one message:
// read it, patch its flags or mailbox, or delete it to trash.
func (s *Server) messageByID(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		s.message(w, r)
	case http.MethodPatch:
		s.updateMessage(w, r)
	case http.MethodDelete:
		s.deleteMessage(w, r)
	default:
		writeMethodNotAllowed(w, "GET, HEAD, PATCH, DELETE")
	}
}

// Message godoc
// @Summary Read one message with its text body
// @Tags messages
// @Security BasicAuth
// @Produce json
// @Param messageID path string true "Mailflow message identifier"
// @Success 200 {object} messageDTO
// @Failure 401 {object} errorBody
// @Failure 404 {object} errorBody
// @Router /v1/messages/{messageID} [get]
func (s *Server) message(w http.ResponseWriter, r *http.Request) {
	if !s.requireGet(w, r) {
		return
	}
	msg, err := s.service.Message(r.Context(), credentials(r.Context()), r.PathValue("messageID"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, messageDTO{
		ID:            msg.ID,
		Subject:       msg.Subject,
		From:          toAddressDTO(msg.From),
		To:            toAddressDTOs(msg.To),
		Cc:            toAddressDTOs(msg.Cc),
		ReceivedAt:    msg.Received,
		Read:          msg.Read,
		Starred:       msg.Starred,
		HasAttachment: msg.HasAttachment,
		Text:          msg.Text,
		TextTruncated: msg.TextTruncated,
	})
}

func toMessageSummaryDTO(m mail.MessageSummary) messageSummaryDTO {
	return messageSummaryDTO{
		ID:            m.ID,
		Subject:       m.Subject,
		From:          toAddressDTO(m.From),
		ReceivedAt:    m.Received,
		Read:          m.Read,
		Starred:       m.Starred,
		HasAttachment: m.HasAttachment,
	}
}

func toAddressDTO(a mail.Address) addressDTO {
	return addressDTO{Name: a.Name, Email: a.Email}
}

// toAddressDTOs always returns an array, never null.
func toAddressDTOs(in []mail.Address) []addressDTO {
	out := make([]addressDTO, 0, len(in))
	for _, a := range in {
		out = append(out, toAddressDTO(a))
	}
	return out
}
