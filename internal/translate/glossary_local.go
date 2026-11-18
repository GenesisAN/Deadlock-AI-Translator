package translate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"deadlocktranslator/internal/settings"
)

const (
	customGlossaryName     = "custom-glossary.txt"
	dictionaryLimit        = 2 << 20
	projectGlossaryVersion = 2
)

var localGlossaryFiles = map[string]string{
	"equipment":          "equipment.json",
	"heroes":             "heroes.json",
	"map":                "map.json",
	"units":              "units.json",
	"events":             "events.json",
	"actions":            "actions.json",
	"chat-abbreviations": "chat-abbreviations.json",
	"user":               "dictionary.json",
}

// LocalGlossaries edits project-bundled dictionaries through local copies.
// The bundled originals remain the fallback after an application update.
type LocalGlossaries struct {
	assets         fs.FS
	dir, legacyDir string
	engine         *Engine
	mu             sync.Mutex
}

func NewLocalGlossaries(assets fs.FS, dir, legacyDir string, engine *Engine) *LocalGlossaries {
	return &LocalGlossaries{assets: assets, dir: dir, legacyDir: legacyDir, engine: engine}
}

func (m *LocalGlossaries) path(name string) string {
	if name == "custom" {
		return filepath.Join(m.dir, customGlossaryName)
	}
	if name == "user" {
		return filepath.Join(m.dir, "dictionary.json")
	}
	return filepath.Join(m.dir, "glossaries", localGlossaryFiles[name]+".patch.json")
}

func (m *LocalGlossaries) Read(name string) (string, error) {
	if name != "custom" && localGlossaryFiles[name] == "" {
		return "", errors.New("未知词库")
	}
	if name != "custom" && name != "user" {
		return m.readProject(name)
	}
	data, err := os.ReadFile(m.path(name))
	if errors.Is(err, os.ErrNotExist) {
		if name == "custom" {
			return "", nil
		}
		if name == "user" {
			data, err = os.ReadFile(filepath.Join(m.legacyDir, "dictionary.json"))
			if errors.Is(err, os.ErrNotExist) {
				return "{\n  \"user\": {\"zh\": {}, \"en\": {}}\n}\n", nil
			}
		}
	}
	if err != nil {
		return "", err
	}
	if len(data) > dictionaryLimit {
		return "", errors.New("词库超过 2 MiB")
	}
	return string(data), nil
}

func (m *LocalGlossaries) readProject(name string) (string, error) {
	base, err := fs.ReadFile(m.assets, "dictionaries/"+localGlossaryFiles[name])
	if err != nil {
		return "", err
	}
	patch, err := os.ReadFile(m.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return string(base), nil
	}
	if err != nil {
		return "", err
	}
	if len(patch) > dictionaryLimit {
		return "", errors.New("词库修改超过 2 MiB")
	}
	var original, changes map[string]any
	if err := json.Unmarshal(base, &original); err != nil {
		return "", err
	}
	if err := json.Unmarshal(patch, &changes); err != nil {
		return "", err
	}
	mergeGlossary(original, changes)
	merged, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		return "", err
	}
	if len(merged) > dictionaryLimit {
		return "", errors.New("合并后的词库超过 2 MiB")
	}
	return string(append(merged, '\n')), nil
}

func mergeGlossary(base, changes map[string]any) {
	for key, value := range changes {
		if value == nil {
			delete(base, key)
			continue
		}
		child, nested := value.(map[string]any)
		original, hasObject := base[key].(map[string]any)
		if nested && base[key] != nil {
			if array, ok := base[key].([]any); ok {
				if spec, marked := child["$lctArray"].(map[string]any); marked {
					base[key] = mergeGlossaryArray(array, spec)
					continue
				}
			}
		}
		if nested && hasObject {
			mergeGlossary(original, child)
		} else {
			base[key] = value
		}
	}
}

func glossaryArrayKey(before, after []any) string {
	for _, field := range []string{"id", "en", "text"} {
		seen := map[string]bool{}
		valid := true
		for _, list := range [][]any{before, after} {
			local := map[string]bool{}
			for _, item := range list {
				object, ok := item.(map[string]any)
				value := fmt.Sprint(object[field])
				if !ok || object[field] == nil || value == "" || local[value] {
					valid = false
					break
				}
				local[value] = true
				seen[value] = true
			}
		}
		if valid && len(seen) > 0 {
			return field
		}
	}
	return ""
}

func glossaryArrayDiff(before, after []any) (map[string]any, bool) {
	field := glossaryArrayKey(before, after)
	if field == "" {
		return nil, false
	}
	old := map[string]map[string]any{}
	newItems := map[string]bool{}
	for _, item := range before {
		object := item.(map[string]any)
		old[fmt.Sprint(object[field])] = object
	}
	changes := map[string]any{}
	added := []string{}
	for _, item := range after {
		object := item.(map[string]any)
		id := fmt.Sprint(object[field])
		newItems[id] = true
		if previous, ok := old[id]; ok {
			if change := glossaryDiff(previous, object); len(change) > 0 {
				changes[id] = change
			}
		} else {
			changes[id] = object
			added = append(added, id)
		}
	}
	removed := []string{}
	for id := range old {
		if !newItems[id] {
			removed = append(removed, id)
		}
	}
	if len(changes) == 0 && len(removed) == 0 {
		return nil, true
	}
	return map[string]any{"$lctArray": map[string]any{"key": field, "changes": changes, "removed": removed, "added": added}}, true
}

func mergeGlossaryArray(base []any, spec map[string]any) []any {
	field, _ := spec["key"].(string)
	changes, _ := spec["changes"].(map[string]any)
	removed := map[string]bool{}
	if values, ok := spec["removed"].([]any); ok {
		for _, value := range values {
			removed[fmt.Sprint(value)] = true
		}
	}
	result := make([]any, 0, len(base)+len(changes))
	for _, item := range base {
		object, ok := item.(map[string]any)
		if !ok {
			result = append(result, item)
			continue
		}
		id := fmt.Sprint(object[field])
		if removed[id] {
			continue
		}
		if change, ok := changes[id].(map[string]any); ok {
			mergeGlossary(object, change)
			delete(changes, id)
		}
		result = append(result, object)
	}
	if order, ok := spec["added"].([]any); ok {
		for _, id := range order {
			key := fmt.Sprint(id)
			if value, exists := changes[key]; exists {
				result = append(result, value)
				delete(changes, key)
			}
		}
	}
	for _, value := range changes {
		result = append(result, value)
	}
	return result
}

func glossaryDiff(base, edited map[string]any) map[string]any {
	changes := map[string]any{}
	for key := range base {
		if _, ok := edited[key]; !ok {
			changes[key] = nil
		}
	}
	for key, value := range edited {
		before, exists := base[key]
		if !exists {
			changes[key] = value
			continue
		}
		oldMap, oldObject := before.(map[string]any)
		newMap, newObject := value.(map[string]any)
		if oldObject && newObject {
			if nested := glossaryDiff(oldMap, newMap); len(nested) > 0 {
				changes[key] = nested
			}
		} else if oldArray, ok := before.([]any); ok {
			if newArray, ok := value.([]any); ok {
				if patch, supported := glossaryArrayDiff(oldArray, newArray); supported {
					if patch != nil {
						changes[key] = patch
					}
					continue
				}
			}
			if !reflect.DeepEqual(before, value) {
				changes[key] = value
			}
		} else if !reflect.DeepEqual(before, value) {
			changes[key] = value
		}
	}
	return changes
}

func (m *LocalGlossaries) Save(name, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if name != "custom" && localGlossaryFiles[name] == "" {
		return errors.New("未知词库")
	}
	if len(content) > dictionaryLimit {
		return errors.New("词库超过 2 MiB")
	}
	if name == "custom" {
		if err := validateCustomGlossary(content); err != nil {
			return err
		}
	} else {
		if err := validateLocalGlossary(name, []byte(content)); err != nil {
			return err
		}
	}
	path := m.path(name)
	if name != "custom" && name != "user" {
		base, err := fs.ReadFile(m.assets, "dictionaries/"+localGlossaryFiles[name])
		if err != nil {
			return err
		}
		var original, edited map[string]any
		if err := json.Unmarshal(base, &original); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(content), &edited); err != nil {
			return err
		}
		patch, err := json.Marshal(glossaryDiff(original, edited))
		if err != nil {
			return err
		}
		content = string(patch)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := settings.WriteAtomic(path, []byte(content)); err != nil {
		return err
	}
	book, err := NewProjectLexicon(m.assets, m.dir, m.legacyDir)
	if err != nil {
		return err
	}
	m.engine.setOfficialLexicon(book)
	return nil
}

func validateLocalGlossary(name string, data []byte) error {
	if !json.Valid(data) {
		return errors.New("词库 JSON 格式无效")
	}
	switch name {
	case "user":
		var p userDictionary
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
	default:
		_, err := parseCategoryGlossary(name, data)
		return err
	}
	return nil
}

// NewProjectLexicon starts from the seven category files shipped with this project.
func NewProjectLexicon(assets fs.FS, dir, legacyDir string) (*Lexicon, error) {
	m := &LocalGlossaries{assets: assets, dir: dir, legacyDir: legacyDir}
	books, err := loadCategoryGlossaries(m.Read)
	if err != nil {
		return nil, err
	}
	user, err := loadUserDictionary(dir, legacyDir)
	if err != nil {
		return nil, err
	}
	custom, err := m.Read("custom")
	if err != nil {
		return nil, err
	}
	if err := validateCustomGlossary(custom); err != nil {
		return nil, err
	}
	book := buildCategoryLexicon(projectGlossaryVersion, books, user.User, user.Learned)
	book.personal = custom
	return book.withGlossary(""), nil
}

func validateCustomGlossary(content string) error {
	seen := map[string]bool{}
	for i, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("词库第 %d 行缺少 =", i+1)
		}
		if err := settings.ValidateContext(settings.Context{Glossary: line}); err != nil {
			return fmt.Errorf("词库第 %d 行: %w", i+1, err)
		}
		normalized := normalize(key)
		if seen[normalized] {
			return fmt.Errorf("词库第 %d 行重复", i+1)
		}
		seen[normalized] = true
	}
	return nil
}

// MigrateContext moves older settings-page entries to the independent file.
func (m *LocalGlossaries) MigrateContext(store *settings.Store) error {
	snap := store.Snapshot()
	old := strings.TrimSpace(snap.Config.Context.Glossary)
	if old == "" {
		return nil
	}
	current, err := m.Read("custom")
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(current, "\n") {
		key, _, ok := strings.Cut(line, "=")
		if ok {
			seen[normalize(key)] = true
		}
	}
	for _, line := range strings.Split(old, "\n") {
		key, _, ok := strings.Cut(line, "=")
		if !ok || seen[normalize(key)] {
			continue
		}
		if strings.TrimSpace(current) != "" {
			current = strings.TrimRight(current, "\n") + "\n"
		}
		current += line + "\n"
		seen[normalize(key)] = true
	}
	if err := m.Save("custom", current); err != nil {
		return err
	}
	context, _ := json.Marshal(settings.Context{Prompt: snap.Config.Context.Prompt})
	_, err = store.Update(map[string]json.RawMessage{"translationContext": context}, snap.Revision)
	return err
}
