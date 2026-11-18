package translate

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"deadlocktranslator/internal/provider"
	"deadlocktranslator/internal/settings"
)

func TestGameAISampleContainsOriginalPromptAndResult(t *testing.T) {
	dir := t.TempDir()
	book := (&Lexicon{}).withGlossary("")
	engine := New(translatorFunc(func(in provider.Input) provider.Output {
		return provider.Output{Translation: "a bad teammate"}
	}), book)
	engine.SetSampleRecorder(NewSampleRecorder(dir))
	snap := settings.Snapshot{Config: settings.Config{
		Provider: "compatible", Timeout: 5000, MaxConcurrent: 2,
		Languages: settings.Languages{Source: "zh-Hans", Target: "en"},
		Providers: map[string]provider.Settings{"compatible": {Model: "test-model"}},
	}, Keys: map[string]string{"compatible": "secret-key"}}
	req := Request{Text: "猪队友", Source: "zh-Hans", Target: "en", Game: true,
		History: []provider.ChatMessage{{Direction: "incoming", Text: "他们在中路"}}}
	for i := 0; i < 2; i++ {
		if _, _, err := engine.Run(context.Background(), snap, req, nil, false, false); err != nil {
			t.Fatal(err)
		}
	}
	// Admin requests are not game chat samples.
	if _, _, err := engine.Run(context.Background(), snap, Request{Text: "另一句", Source: "zh-Hans", Target: "en"}, nil, false, false); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(dir, "translation-samples", "*.jsonl"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("sample files = %v, %v", paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if bytes := strings.Count(string(data), "\n"); bytes != 1 {
		t.Fatalf("cached or admin translation was recorded: %q", data)
	}
	if strings.Contains(string(data), "secret-key") {
		t.Fatal("API key leaked into the sample")
	}
	var sample TranslationSample
	if err := json.Unmarshal(data, &sample); err != nil {
		t.Fatal(err)
	}
	if sample.SchemaVersion != 1 || sample.Original != "猪队友" || sample.Translation != "a bad teammate" ||
		sample.Direction != "incoming" || sample.Provider != "compatible" || sample.Model != "test-model" ||
		len(sample.Messages) != 2 || sample.Messages[1].Content != "猪队友" ||
		!strings.Contains(sample.Messages[0].Content, "他们在中路") {
		t.Fatalf("incomplete game AI sample: %+v", sample)
	}
}

func TestSampleRecorderKeepsConcurrentLinesIntact(t *testing.T) {
	dir := t.TempDir()
	recorder := NewSampleRecorder(dir)
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Go(func() {
			if err := recorder.Append(TranslationSample{Original: fmt.Sprintf("line %d", i), Translation: "translated"}); err != nil {
				t.Errorf("append: %v", err)
			}
		})
	}
	workers.Wait()
	paths, err := filepath.Glob(filepath.Join(dir, "translation-samples", "*.jsonl"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("sample files = %v, %v", paths, err)
	}
	f, err := os.Open(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var sample TranslationSample
		if err := json.Unmarshal(scanner.Bytes(), &sample); err != nil || sample.Original == "" {
			t.Fatalf("broken sample line %d: %s (%v)", count, scanner.Text(), err)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 20 {
		t.Fatalf("recorded %d of 20 samples", count)
	}
}
