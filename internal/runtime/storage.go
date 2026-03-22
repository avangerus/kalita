package runtime

import (
	"io"
	"math/rand"
	"sync"
	"time"

	"kalita/internal/blob"
	"kalita/internal/catalog"
	"kalita/internal/schema"

	"github.com/oklog/ulid/v2"
)

type Record struct {
	ID        string                 `json:"id"`
	Version   int64                  `json:"version"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
	Deleted   bool                   `json:"-"`
	Data      map[string]interface{} `json:"data"`
}

type Storage struct {
	Mu             sync.RWMutex
	Schemas        map[string]*schema.Entity     // FQN ("module.name") -> схема
	Data           map[string]map[string]*Record // FQN -> id -> запись
	ActionRequests map[string]*WorkflowActionRequest
	Enums          map[string]catalog.EnumDirectory // каталог enum'ов (если нужен на валидации/UI)
	entropy        io.Reader
	Blob           blob.BlobStore
}

// NewStorage наполняет схемы/энумы и готов к работе
func NewStorage(entities []*schema.Entity, enumCatalog map[string]catalog.EnumDirectory) *Storage {
	src := rand.New(rand.NewSource(time.Now().UnixNano()))
	s := &Storage{
		Schemas:        make(map[string]*schema.Entity),
		Data:           make(map[string]map[string]*Record),
		ActionRequests: make(map[string]*WorkflowActionRequest),
		Enums:          enumCatalog,
		entropy:        ulid.Monotonic(src, 0),
	}
	for _, e := range entities {
		fqn := e.Module + "." + e.Name
		s.Schemas[fqn] = e
	}
	return s
}

func (s *Storage) NewID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now()), s.entropy).String()
}

func (s *Storage) Exists(entity, id string) bool {
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	m := s.Data[entity]
	if m == nil {
		return false
	}
	rec := m[id]
	return rec != nil && !rec.Deleted
}

// FindIncomingRefs возвращает первую найденную входящую ссылку на (targetEntityFQN, targetID).
// Если ссылок нет — ok=false.
func (s *Storage) FindIncomingRefs(targetEntityFQN, targetID string) (refEntityFQN, refField string, ok bool) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()

	for refFQN, schema := range s.Schemas {
		records := s.Data[refFQN]
		if records == nil {
			continue
		}
		for _, f := range schema.Fields {
			// 1) Определяем целевой FQN для поля-ссылки
			var wantFQN string
			if f.Type == "ref" && f.RefTarget != "" {
				if fq, ok := s.NormalizeEntityName("", f.RefTarget); ok {
					wantFQN = fq
				} else {
					// если RefTarget неразрешим — пропустим это поле
					continue
				}
				if wantFQN != targetEntityFQN {
					continue
				}
				// Проверка одиночной ссылки
				for _, rec := range records {
					if rec == nil || rec.Deleted {
						continue
					}
					if id, _ := rec.Data[f.Name].(string); id == targetID {
						return refFQN, f.Name, true
					}
				}
			}

			// 2) Массив ссылок: array[ref[...]]
			if f.Type == "array" && f.ElemType == "ref" && f.RefTarget != "" {
				if fq, ok := s.NormalizeEntityName("", f.RefTarget); ok {
					wantFQN = fq
				} else {
					continue
				}
				if wantFQN != targetEntityFQN {
					continue
				}
				for _, rec := range records {
					if rec == nil || rec.Deleted {
						continue
					}
					// Значение может быть []interface{} или []string
					if arr, ok := rec.Data[f.Name].([]interface{}); ok {
						for _, it := range arr {
							if id, _ := it.(string); id == targetID {
								return refFQN, f.Name, true
							}
						}
					} else if arrS, ok := rec.Data[f.Name].([]string); ok {
						for _, id := range arrS {
							if id == targetID {
								return refFQN, f.Name, true
							}
						}
					}
				}
			}
		}
	}
	return "", "", false
}
