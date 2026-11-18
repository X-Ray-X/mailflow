package api

import (
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

// attachmentDTO is the metadata a caller sees. There is deliberately no blob
// identifier or download address: the message identifier and position are the
// only addressing values in this contract.
type attachmentDTO struct {
	Position    int    `json:"position"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
}

type attachmentsResponse struct {
	Attachments []attachmentDTO `json:"attachments"`
}

// attachments lists the downloadable files of one message.
func (s *Server) attachments(w http.ResponseWriter, r *http.Request) {
	if !s.requireGet(w, r) {
		return
	}
	list, err := s.service.Attachments(r.Context(), credentials(r.Context()), r.PathValue("messageID"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	resp := attachmentsResponse{Attachments: make([]attachmentDTO, 0, len(list))}
	for _, att := range list {
		resp.Attachments = append(resp.Attachments, attachmentDTO{
			Position:    att.Position,
			Filename:    att.Filename,
			ContentType: att.ContentType,
			Size:        att.Size,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// downloadAttachment streams one addressed attachment. A declared length lets
// a client detect a download that ended early, and the bounded copy keeps an
// upstream response from growing without limit.
func (s *Server) downloadAttachment(w http.ResponseWriter, r *http.Request) {
	if !s.requireGet(w, r) {
		return
	}
	raw := r.PathValue("position")
	position, err := strconv.Atoi(raw)
	if err != nil || position < 0 || strconv.Itoa(position) != raw {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "attachment position must be a non-negative integer")
		return
	}

	download, err := s.service.DownloadAttachment(r.Context(), credentials(r.Context()), r.PathValue("messageID"), position)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer func() { _ = download.Body.Close() }()

	contentType := download.ContentType
	if _, _, err := mime.ParseMediaType(contentType); err != nil {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
		"filename": safeFilename(download.Filename),
	}))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.FormatInt(download.Size, 10))
	if r.Method == http.MethodHead {
		return
	}

	written, err := io.CopyN(w, download.Body, download.Size)
	if err != nil || written != download.Size {
		// Headers are already sent, so the client detects the short body from
		// Content-Length and must treat the download as failed.
		s.log.Warn("attachment download ended early", "written", written, "expected", download.Size, "error", err)
	}
}

// safeFilename removes header-breaking characters from a vendor-supplied name
// and bounds its length. mime.FormatMediaType then quotes the result.
func safeFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' || r == '\\' || r == ':' || r == ';' {
			return -1
		}
		return r
	}, name)
	runes := []rune(strings.TrimSpace(name))
	if len(runes) == 0 {
		return "attachment"
	}
	if len(runes) > 255 {
		runes = runes[:255]
	}
	return string(runes)
}
