package translate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"deadlocktranslator/internal/provider"
)

// TranslationSample records one successful game chat translation made by AI.
// Messages are the exact system and user messages sent to the model.
type TranslationSample struct {
	SchemaVersion int                `json:"schemaVersion"`
	RecordedAt    time.Time          `json:"recordedAt"`
	Direction     string             `json:"direction"`
	Original      string             `json:"original"`
	Source        string             `json:"sourceLanguage"`
	Target        string             `json:"targetLanguage"`
	Provider      string             `json:"provider"`
	Model         string             `json:"model"`
	Fallback      bool               `json:"fallback,omitempty"`
	Messages      []provider.Message `json:"messages"`
	Translation   string             `json:"translation"`
}

// SampleRecorder keeps local JSONL files separated by day. It does not retain
// credentials, endpoints, or a second in-memory copy of the chat data.
type SampleRecorder struct {
	dir string
	mu  sync.Mutex
}

func NewSampleRecorder(stateDir string) *SampleRecorder {
	return &SampleRecorder{dir: filepath.Join(stateDir, "translation-samples")}
}

func (r *SampleRecorder) Append(sample TranslationSample) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := os.MkdirAll(r.dir, 0700); err != nil {
		return err
	}
	if sample.RecordedAt.IsZero() {
		sample.RecordedAt = time.Now()
	}
	sample.SchemaVersion = 1
	line, err := json.Marshal(sample)
	if err != nil {
		return err
	}
	path := filepath.Join(r.dir, sample.RecordedAt.Local().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(append(line, '\n'))
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
