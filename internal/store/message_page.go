package store

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

type messageBoundary struct {
	Updated string `json:"updated"`
	ID      string `json:"id"`
}
type MessagePage struct {
	Messages []Doc  `json:"messages"`
	Before   string `json:"messageBefore"`
	HasMore  bool   `json:"messagesHaveMore"`
}

// MessagesBefore returns a bounded page in display order, using the same index
// as session history. A cursor is scoped by the caller's session, never a SQL
// fragment. The complete durable transcript remains in SQLite.
func (s *Store) MessagesBefore(sessionID, before string, limit int) (MessagePage, error) {
	p := MessagePage{Messages: []Doc{}}
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	args := []any{sessionID}
	query := `SELECT body,updated,id FROM entities WHERE kind='message' AND json_extract(body,'$.sessionId')=?`
	if before != "" {
		if len(before) > 2048 {
			return p, errors.New("invalid message cursor")
		}
		b, err := base64.RawURLEncoding.DecodeString(before)
		if err != nil {
			return p, errors.New("invalid message cursor")
		}
		var boundary messageBoundary
		if json.Unmarshal(b, &boundary) != nil || boundary.Updated == "" || boundary.ID == "" {
			return p, errors.New("invalid message cursor")
		}
		query += ` AND (updated,id)<(?,?)`
		args = append(args, boundary.Updated, boundary.ID)
	}
	query += ` ORDER BY updated DESC,id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	bytes := 0
	for rows.Next() {
		var body string
		var boundary messageBoundary
		if err = rows.Scan(&body, &boundary.Updated, &boundary.ID); err != nil {
			return p, err
		}
		if len(p.Messages) >= limit || len(p.Messages) > 0 && bytes+len(body) > 2<<20 {
			p.HasMore = true
			break
		}
		var d Doc
		if err = json.Unmarshal([]byte(body), &d); err != nil {
			return p, err
		}
		p.Messages = append(p.Messages, d)
		bytes += len(body)
		b, _ := json.Marshal(boundary)
		p.Before = base64.RawURLEncoding.EncodeToString(b)
	}
	for i, j := 0, len(p.Messages)-1; i < j; i, j = i+1, j-1 {
		p.Messages[i], p.Messages[j] = p.Messages[j], p.Messages[i]
	}
	return p, rows.Err()
}
