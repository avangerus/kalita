package api

import (
	"io"
	"math/rand"
	"sync"
	"time"

	"kalita/internal/dsl"
	"kalita/internal/reference"

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
	mu      sync.RWMutex
	Schemas map[string]*dsl.Entity             // FQN ("module.name") -> схема
	Data    map[string]map[string]*Record      // FQN -> id -> запись
	Enums   map[string]reference.EnumDirectory // каталог enum'ов (если нужен на валидации/UI)
	entropy io.Reader
}

// NewStorage наполняет схемы/энумы и готов к работе
func NewStorage(entities []*dsl.Entity, enumCatalog map[string]reference.EnumDirectory) *Storage {
	src := rand.New(rand.NewSource(time.Now().UnixNano()))
	s := &Storage{
		Schemas: make(map[string]*dsl.Entity),
		Data:    make(map[string]map[string]*Record),
		Enums:   enumCatalog,
		entropy: ulid.Monotonic(src, 0),
	}
	for _, e := range entities {
		fqn := e.Module + "." + e.Name
		s.Schemas[fqn] = e
	}
	return s
}

func (s *Storage) newID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now()), s.entropy).String()
}

func (s *Storage) Exists(entity, id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
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
	s.mu.RLock()
	defer s.mu.RUnlock()

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
