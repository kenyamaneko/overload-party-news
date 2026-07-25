package pubsubpush

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-news/internal/port"
)

// errMalformedEnvelope は HTTP body が Pub/Sub push envelope の形を満たさないことを示す。
var errMalformedEnvelope = errors.New("pubsubpush: malformed envelope")

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

// Handle は push envelope を decode して handle に委譲する。
//   - envelope 自体が push envelope の形を満たさない: 400
//   - message.data が base64 として復号できない: 200 (ack) + warn ログ
//   - handle が nil を返す: 200 (ack)
//   - handle がエラーを返す: 500 (Pub/Sub 側が再配送する)
func (h *Handler) Handle(c *gin.Context) {
	var env envelope
	if err := c.ShouldBindJSON(&env); err != nil {
		respondErr := fmt.Errorf("%w: %s", errMalformedEnvelope, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": respondErr.Error()})
		return
	}

	data, err := base64.StdEncoding.DecodeString(env.Message.Data)
	if err != nil {
		slog.WarnContext(c.Request.Context(), "pubsubpush: undecodable data, acking", "error", err)
		c.Status(http.StatusOK)
		return
	}

	if err := h.handle(c.Request.Context(), data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusOK)
}
