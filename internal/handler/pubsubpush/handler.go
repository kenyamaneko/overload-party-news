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

// Handler は Pub/Sub push subscription の受け口を担う HTTP delivery 層。
type Handler struct {
	handle port.MessageHandler
}

// NewHandler は Handler を生成する。
func NewHandler(handle port.MessageHandler) *Handler {
	return &Handler{handle: handle}
}

// Handle は push envelope を解析・復号し、既存の port.MessageHandler へ委譲する。
func (h *Handler) Handle(c *gin.Context) {
	var env envelope
	// envelope の形式不正・復号不能は再配送しても解消しないため、200 で握りつぶさず 400 で dead letter に送り内容を確認できるようにする。
	if err := c.ShouldBindJSON(&env); err != nil {
		respondErr := fmt.Errorf("%w: %s", errMalformedEnvelope, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": respondErr.Error()})
		return
	}
	if env.Message.Data == "" {
		respondErr := fmt.Errorf("%w: message.data is empty", errMalformedEnvelope)
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
