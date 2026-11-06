package app

import "errors"

func (c *Core) earlierSessionMessages(p Doc) (Doc, error) {
	id := str(p, "id")
	if _, err := c.Store.Get("session", id); err != nil {
		return nil, err
	}
	cursor, err := c.Store.SessionMessageCursor(id)
	if err != nil {
		return nil, err
	}
	if str(p, "revision") == "" || str(p, "revision") != cursor {
		return nil, errors.New("conversation changed; refresh before loading earlier messages")
	}
	page, err := c.Store.MessagesBefore(id, str(p, "before"), number(p, "limit", 100))
	if err != nil {
		return nil, err
	}
	after, err := c.Store.SessionMessageCursor(id)
	if err != nil {
		return nil, err
	}
	if after != cursor {
		return nil, errors.New("conversation changed while loading history; refresh to retry")
	}
	return Doc{"messages": page.Messages, "messageBefore": page.Before, "messagesHaveMore": page.HasMore, "messageCursor": cursor}, nil
}
