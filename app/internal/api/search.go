package api

import (
	"net/http"
	"strings"

	"github.com/X-Ray-X/mailflow/app/internal/mail"
)

// search answers a text query with the same page shape a mailbox listing uses.
// Only the contract's own parameters are accepted; a vendor filter name cannot
// be smuggled in as a query parameter.
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	if !s.requireGet(w, r) {
		return
	}
	query := r.URL.Query()
	for name := range query {
		switch name {
		case "q", "mailboxId", "limit", "cursor":
		default:
			writeError(w, http.StatusBadRequest, codeInvalidRequest, "request contains an unknown parameter")
			return
		}
	}
	text := strings.TrimSpace(query.Get("q"))
	if text == "" {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "q is required and must not be empty")
		return
	}
	limit, err := s.pageSize(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
		return
	}

	page, err := s.service.Search(r.Context(), credentials(r.Context()), mail.SearchRequest{
		Text:      text,
		MailboxID: query.Get("mailboxId"),
		Page: mail.PageRequest{
			Limit:  limit,
			Cursor: query.Get("cursor"),
		},
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
