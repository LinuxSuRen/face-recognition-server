package store

import (
	"context"
	"database/sql"
	"errors"
)

// LoginEvent records one authentication attempt that opened a session.
type LoginEvent struct {
	ID            int64   `json:"id"`
	UserID        int64   `json:"user_id"`
	Username      string  `json:"username"`
	Method        string  `json:"method"` // face | password | oidc
	Similarity    float64 `json:"similarity"`
	HasImage      bool    `json:"has_image"`
	ImageMimeType string  `json:"image_mime_type"`
	IPAddress     string  `json:"ip"`
	CreatedAt     string  `json:"created_at"`
}

// CreateLoginEvent stores one successful login; image may be nil.
func (s *Store) CreateLoginEvent(ctx context.Context, userID int64,
	method string, similarity float64, image []byte, mimeType, ip string) error {
	var imageValue any
	if len(image) > 0 {
		imageValue = image
	} else {
		imageValue = nil
	}
	var mimeValue any
	if mimeType != "" {
		mimeValue = mimeType
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO login_events
		    (user_id, method, similarity, image, image_mime_type, ip, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, method, similarity, imageValue, mimeValue, ip, utcNow())
	return err
}

// ListLoginEvents returns one page of login records (newest first).
func (s *Store) ListLoginEvents(ctx context.Context, page, pageSize int) ([]LoginEvent, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM login_events").Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.user_id, u.username, e.method, e.similarity,
		       e.image IS NOT NULL, COALESCE(e.image_mime_type, ''),
		       COALESCE(e.ip, ''), e.created_at
		FROM login_events e
		JOIN users u ON u.id = e.user_id
		ORDER BY e.id DESC
		LIMIT ? OFFSET ?`, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	events := []LoginEvent{}
	for rows.Next() {
		var event LoginEvent
		if err := rows.Scan(&event.ID, &event.UserID, &event.Username,
			&event.Method, &event.Similarity, &event.HasImage,
			&event.ImageMimeType, &event.IPAddress, &event.CreatedAt); err != nil {
			return nil, 0, err
		}
		events = append(events, event)
	}
	return events, total, rows.Err()
}

// GetLoginEventImage returns the stored photo for one login record.
func (s *Store) GetLoginEventImage(ctx context.Context, id int64) ([]byte, string, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT image, COALESCE(image_mime_type, 'image/jpeg') FROM login_events WHERE id = ?", id)
	var image []byte
	var mimeType string
	err := row.Scan(&image, &mimeType)
	if errors.Is(err, sql.ErrNoRows) || image == nil {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return image, mimeType, nil
}
