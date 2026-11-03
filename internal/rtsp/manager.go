package rtsp

import (
	"context"
	"errors"
	"sync"
)

// Manager tracks the live sessions and the configured video source.
type Manager struct {
	mu         sync.Mutex
	sessions   map[string]*Session
	maxSession int
	url        string
	transport  string
}

// ErrTooManySessions marks a full manager.
var ErrTooManySessions = errors.New("RTSP 会话数量已达上限")

// ErrSessionNotFound marks an unknown session id.
var ErrSessionNotFound = errors.New("RTSP 会话不存在")

// ErrSourceNotConfigured marks a missing RTSP source configuration.
var ErrSourceNotConfigured = errors.New("RTSP 视频源尚未配置")

// NewManager creates a manager allowing at most max concurrent sessions
// (each one owns an ffmpeg process).
func NewManager(max int) *Manager {
	if max <= 0 {
		max = 4
	}
	return &Manager{sessions: map[string]*Session{}, maxSession: max}
}

// Configure stores the single video source used by new sessions.
func (m *Manager) Configure(url, transport string) {
	m.mu.Lock()
	m.url = url
	m.transport = transport
	m.mu.Unlock()
}

// Source returns the configured video source.
func (m *Manager) Source() (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.url == "" {
		return "", "", ErrSourceNotConfigured
	}
	transport := m.transport
	if transport == "" {
		transport = "tcp"
	}
	return m.url, transport, nil
}

// Create starts a new session for the configured source.
func (m *Manager) Create(ctx context.Context, opts SessionOptions) (*Session, error) {
	m.mu.Lock()
	if len(m.sessions) >= m.maxSession {
		m.mu.Unlock()
		return nil, ErrTooManySessions
	}
	if opts.URL == "" {
		opts.URL = m.url
		opts.Transport = m.transport
	}
	if opts.URL == "" {
		m.mu.Unlock()
		return nil, ErrSourceNotConfigured
	}
	if opts.Transport == "" {
		opts.Transport = "tcp"
	}
	session := newSession(opts)
	m.sessions[session.ID()] = session
	m.mu.Unlock()

	session.start(ctx)
	return session, nil
}

// Get returns the session or ErrSessionNotFound.
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, exists := m.sessions[id]
	if !exists {
		return nil, ErrSessionNotFound
	}
	return session, nil
}

// Stop terminates and removes the session.
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	session, exists := m.sessions[id]
	if exists {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if !exists {
		return ErrSessionNotFound
	}
	session.Stop()
	return nil
}

// StopAll terminates every session (used on shutdown).
func (m *Manager) StopAll() {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.sessions = map[string]*Session{}
	m.mu.Unlock()
	for _, session := range sessions {
		session.Stop()
	}
}

// Count returns the active session count.
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}
