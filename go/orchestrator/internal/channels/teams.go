package channels

import (
	"fmt"
	"net/http"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/daemon"
)

func handleTeamsWebhook(_ http.ResponseWriter, _ *http.Request, _ *Channel) (*daemon.MessagePayload, error) {
	return nil, fmt.Errorf("Teams webhook handler not yet implemented")
}
