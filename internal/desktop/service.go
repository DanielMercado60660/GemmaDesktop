package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"gemmadesktop.local/app/internal/app"
)

// Service deliberately exposes only the narrow application façade to WebKit.
type Service struct{ core *app.Core }

func New(core *app.Core) *Service { return &Service{core: core} }
func (s *Service) Call(method string, payload string) (string, error) {
	if len(payload) > 4<<20 {
		return "", errors.New("Request exceeds 4 MiB")
	}
	p := app.Doc{}
	if payload != "" {
		if e := json.Unmarshal([]byte(payload), &p); e != nil {
			return "", e
		}
	}
	v, e := s.core.Call(context.Background(), method, p)
	if e != nil {
		return "", e
	}
	b, e := json.Marshal(v)
	return string(b), e
}
