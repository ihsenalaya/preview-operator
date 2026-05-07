package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Server is the Copilot Extension HTTP handler.
type Server struct {
	crClient      client.Client
	kubeClient    kubernetes.Interface
	webhookSecret string
}

func NewServer(crClient client.Client, kubeClient kubernetes.Interface, webhookSecret string) *Server {
	return &Server{
		crClient:      crClient,
		kubeClient:    kubeClient,
		webhookSecret: webhookSecret,
	}
}

// copilotRequest is the OpenAI-compatible message format sent by GitHub Copilot.
type copilotRequest struct {
	Messages []copilotMessage `json:"messages"`
}

type copilotMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// sseChunk is one OpenAI streaming chunk.
type sseChunk struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []sseChoice `json:"choices"`
}

type sseChoice struct {
	Index        int      `json:"index"`
	Delta        sseDelta `json:"delta"`
	FinishReason *string  `json:"finish_reason"`
}

type sseDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/previews/") {
		s.serveCheckpointAPI(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	var req copilotRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Extract last user message
	userMsg := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			userMsg = req.Messages[i].Content
			break
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	response := s.execute(ctx, userMsg)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	msgID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	s.writeSSE(w, msgID, "assistant", response)
	s.writeSSEDone(w, msgID)

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) writeSSE(w http.ResponseWriter, id, role, content string) {
	stop := "stop"
	chunks := []sseChunk{
		{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   "cellenza-extension",
			Choices: []sseChoice{{
				Index: 0,
				Delta: sseDelta{Role: role, Content: content},
			}},
		},
		{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   "cellenza-extension",
			Choices: []sseChoice{{
				Index:        0,
				Delta:        sseDelta{},
				FinishReason: &stop,
			}},
		},
	}
	for _, chunk := range chunks {
		data, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	}
}

func (s *Server) writeSSEDone(w http.ResponseWriter, _ string) {
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
}

// execute parses the user message and dispatches to the right command.
func (s *Server) execute(ctx context.Context, msg string) string {
	msg = strings.TrimSpace(msg)
	// Strip @cellenza prefix
	for _, prefix := range []string{"@cellenza ", "@cellenza"} {
		if rest, ok := strings.CutPrefix(msg, prefix); ok {
			msg = rest
			break
		}
	}
	msg = strings.TrimSpace(msg)

	parts := strings.Fields(msg)
	if len(parts) == 0 {
		return cmdHelp()
	}

	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	switch cmd {
	case "status":
		return s.cmdStatus(ctx, args)
	case "logs":
		return s.cmdLogs(ctx, args)
	case "extend":
		return s.cmdExtend(ctx, args)
	case "wake":
		return s.cmdWake(ctx, args)
	case "reset-db", "resetdb":
		return s.cmdResetDB(ctx, args)
	case "save-db", "savedb":
		return s.cmdSaveDB(ctx, args)
	case "restore-db", "restoredb":
		return s.cmdRestoreDB(ctx, args)
	case "list-checkpoints", "listcheckpoints":
		return s.cmdListCheckpoints(ctx, args)
	case "run-sql", "runsql":
		return s.cmdRunSQL(ctx, args)
	case "retest-ai", "retestai":
		return s.cmdRetestAI(ctx, args)
	case "enrich":
		return s.cmdEnrich(ctx, args)
	case "set-prompt", "setprompt":
		return s.cmdSetPrompt(ctx, args)
	case "show-prompt", "showprompt":
		return s.cmdShowPrompt(ctx, args)
	case "list":
		return s.cmdList(ctx)
	case "help", "":
		return cmdHelp()
	default:
		return fmt.Sprintf("Commande inconnue: `%s`\n\n%s", cmd, cmdHelp())
	}
}
