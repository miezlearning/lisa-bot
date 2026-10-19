package router

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/types"

	"whatsapp-bot/internal/nlu"
)

// MessageContext holds request data dispatched to module handlers.
type MessageContext struct {
	Context      context.Context
	SenderJID    types.JID
	SenderPhone  string
	SenderName   string
	ChatJID      types.JID
	IsGroup      bool
	RawMessage   string
	CleanMessage string
	History      []nlu.ChatMessage
	NLUResult    *nlu.NLUResult
}

// ModuleHandler defines the interface for modular domain handlers.
type ModuleHandler interface {
	Name() string
	CanHandle(result *nlu.NLUResult) bool
	Execute(ctx *MessageContext) (string, error)
}

// Router dispatches incoming parsed messages to registered module handlers.
type Router struct {
	handlers []ModuleHandler
}

// NewRouter creates an empty router.
func NewRouter() *Router {
	return &Router{
		handlers: make([]ModuleHandler, 0),
	}
}

// Register adds a new module handler to the registry.
func (r *Router) Register(h ModuleHandler) {
	r.handlers = append(r.handlers, h)
}

// Route finds the first handler that CanHandle the result and executes it.
func (r *Router) Route(ctx *MessageContext) (string, error) {
	if ctx == nil || ctx.NLUResult == nil {
		return "", fmt.Errorf("invalid message context")
	}

	for _, h := range r.handlers {
		if h.CanHandle(ctx.NLUResult) {
			resp, err := h.Execute(ctx)
			if err != nil {
				return "", fmt.Errorf("module %s error: %w", h.Name(), err)
			}
			return resp, nil
		}
	}

	// Default fallback response if no module handles the intent
	return "Maaf, Lisa belum memahami pertanyaanmu. Coba tanyakan *jadwal kuliah*, *larangan praktikum*, atau ketik *menu* untuk melihat panduan.", nil
}

// FormatAutoCorrectHeader adds a transparency auto-correct header if any words were adjusted.
func FormatAutoCorrectHeader(corrections []string) string {
	if len(corrections) == 0 {
		return ""
	}

	// Format: _(Auto-correct: "iru" -> "itu")_
	joined := strings.Join(corrections, ", ")
	return fmt.Sprintf("_(Auto-correct: %s)_\n\n", joined)
}
