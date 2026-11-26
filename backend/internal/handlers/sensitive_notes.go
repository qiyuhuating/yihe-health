package handlers

import "strings"
import "yihe-care/internal/securestore"

func (h *Handler) sealNote(note string) (string, error) {
	if h.Box == nil {
		return note, nil
	} // Explicit legacy/local mode; deployment requires a key.
	return h.Box.Seal(note, "care-text")
}
func (h *Handler) openNote(note string) (string, error) {
	if !strings.HasPrefix(note, securestore.Prefix) {
		return note, nil
	}
	return h.Box.Open(note, "care-text")
}
