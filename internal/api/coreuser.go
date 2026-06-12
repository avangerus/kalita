package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

// core.User is the built-in people directory: a read-only entity whose records
// are the node's registered actors (humans and agents). It is not event-sourced
// like a pack entity — it projects the identity registry — so ref[core.User]
// pickers and labels resolve to real people without a User table in every pack.
//
// These helpers let the generic record endpoints serve "core.User" by reading
// the registry instead of the engine. Read-only: users are created through the
// identity flow (register / invite), not the record API.

const coreUserEntity = "core.User"

// coreUserRecord shapes one actor as a record {id, entity, values}.
func coreUserRecord(id, role, typ string) map[string]any {
	return map[string]any{
		"id":     id,
		"entity": coreUserEntity,
		"values": map[string]any{"id": id, "name": id, "role": role, "type": typ},
	}
}

// coreUserList returns the directory, filtered by a case-insensitive search over
// id and role, capped at limit (0 = no cap). Disabled actors are omitted.
func (s *Server) coreUserList(ctx context.Context, search string, limit int) ([]map[string]any, error) {
	if s.reg == nil {
		return []map[string]any{}, nil
	}
	actors, err := s.reg.List(ctx)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(search))
	out := []map[string]any{}
	for _, a := range actors {
		if a.Disabled {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(a.ID), q) && !strings.Contains(strings.ToLower(a.Role), q) {
			continue
		}
		out = append(out, coreUserRecord(a.ID, a.Role, string(a.Type)))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// serveCoreUserList writes the directory as a records list (any authenticated
// actor may read it — a picker needs it).
func (s *Server) serveCoreUserList(w http.ResponseWriter, r *http.Request, search string, limit int) {
	rows, err := s.coreUserList(r.Context(), search, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": rows})
}

// serveCoreUserGet writes one directory entry by id.
func (s *Server) serveCoreUserGet(w http.ResponseWriter, r *http.Request, id string) {
	if s.reg == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "NOT_FOUND", "message": "no directory on this node"})
		return
	}
	info, err := s.reg.Get(r.Context(), id)
	if err != nil || info == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "NOT_FOUND", "message": "no such user " + id})
		return
	}
	writeJSON(w, http.StatusOK, coreUserRecord(info.ID, info.Role, string(info.Type)))
}
