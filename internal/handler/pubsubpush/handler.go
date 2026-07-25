package pubsubpush

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-news/internal/port"
)

// errMalformedEnvelope は HTTP body が Pub/Sub push envelope の形を満たさないことを示す。
var errMalformedEnvelope = errors.New("pubsubpush: malformed envelope")

// errUndecodableData は envelope の message.data が base64 として復号できないことを示す。
var errUndecodableData = errors.New("pubsubpush: undecodable data")

// envelope は Cloud Pub/Sub push subscription が送る HTTP body の形。
type envelope struct {
	Message struct {
		Data string `json:"data"`
	} `json:"message"`
}

// Handler は Pub/Sub push subscription からの HTTP POST を既存の port.MessageHandler に橋渡しする。
type Handler struct {
	handle port.MessageHandler
}

// NewHandler は Handler を生成する。
func NewHandler(handle port.MessageHandler) *Handler {
	return &Handler{handle: handle}
}

// Handle は Pub/Sub push subscription からの HTTP POST を受け、既存の port.MessageHandler に委譲する。
func (h *Handler) Handle(c *gin.Context) {
	var env envelope
	if err := c.ShouldBindJSON(&env); err != nil {
		respondErr := fmt.Errorf("%w: %s", errMalformedEnvelope, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": respondErr.Error()})
		return
	}

	data, err := base64.StdEncoding.DecodeString(env.Message.Data)
	if err != nil {
		respondErr := fmt.Errorf("%w: %s", errUndecodableData, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": respondErr.Error()})
		return
	}

	if err := h.handle(c.Request.Context(), data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusOK)
}
