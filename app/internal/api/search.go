package api

import (
	"net/http"
	"strings"

	"github.com/X-Ray-X/mailflow/app/internal/mail"
)

// Search godoc
// @Summary Search mail by text
// @Description Bounded, paged search using the same page shape as message listing.
// @Tags messages
// @Security BasicAuth
// @Produce json
// @Param q query string true "Non-empty search text"
// @Param mailboxId query string false "Restrict to one Mailflow mailbox"
// @Param limit query int false "Page size"
// @Param cursor query string false "Next cursor from a previous page"
// @Success 200 {object} messagesResponse
// @Failure 400 {object} errorBody
// @Failure 401 {object} errorBody
// @Failure 404 {object} errorBody
// @Router /v1/search [get]
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
