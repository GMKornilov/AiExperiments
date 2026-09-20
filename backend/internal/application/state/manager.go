// Package state serializes single-process commits and cancellation ownership.
package state

import (
	"context"
	"errors"
	"sync"

	"aichallenge/week_1/task_1/internal/domain/model"
)

var ErrStorage = errors.New("хранилище памяти недоступно")

// Committer must save the complete candidate atomically or leave storage intact.
type Committer interface{ Save(model.State) error }
type titleCall struct {
	sid, pid, cid string
	cancel        context.CancelFunc
}
type Manager struct {
	abandoned map[string]bool
	titles    map[string]titleCall
	mu        sync.Mutex
	value     model.State
	disk      Committer
	closed    bool
	cancels   map[string]context.CancelFunc
}

func New(value model.State, disk Committer) *Manager {
	return &Manager{value: value.Clone(), disk: disk, cancels: map[string]context.CancelFunc{}}
}
func (m *Manager) Snapshot() model.State { m.mu.Lock(); defer m.mu.Unlock(); return m.value.Clone() }
func (m *Manager) Update(change func(*model.State) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrStorage
	}
	candidate := m.value.Clone()
	// A failed save may leave a durable lease after its worker has exited. The
	// next explicit mutation can reclaim that lease without publishing any result.
	for _, b := range candidate.Sessions {
		if op := b.Pending; op != nil && m.abandoned[op.Generation] {
			if p := b.Projects[op.ProjectID]; p != nil {
				if c := p.Chats[op.ChatID]; c != nil {
					if saved := c.Operations[op.ID]; saved != nil {
						saved.Status = "failed"
					}
				}
			}
			b.Pending = nil
		}
	}
	if err := change(&candidate); err != nil {
		return err
	}
	if err := m.disk.Save(candidate); err != nil {
		return ErrStorage
	}
	m.value = candidate.Clone()
	live := map[string]bool{}
	for _, b := range m.value.Sessions {
		if b.Pending != nil {
			live[b.Pending.Generation] = true
		}
	}
	for generation, cancel := range m.cancels {
		if !live[generation] {
			cancel()
			delete(m.cancels, generation)
		}
	}
	for key, call := range m.titles {
		c := m.value.Chat(call.sid, call.pid, call.cid)
		if c == nil || c.TitleStatus != "pending" {
			call.cancel()
			delete(m.titles, key)
		}
	}
	return nil
}

// Attach handles pause/delete racing the start of an external call.
func (m *Manager) Attach(sid string, op model.Operation, cancel context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || !m.value.Owns(sid, op) {
		cancel()
		return
	}
	m.cancels[op.Generation] = cancel
}
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for _, call := range m.titles {
		call.cancel()
	}
	for _, cancel := range m.cancels {
		cancel()
	}
	m.cancels = map[string]context.CancelFunc{}
}
func (m *Manager) StorageError() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrStorage
	}
	return nil
}

func (m *Manager) AttachTitle(sid, pid, cid string, cancel context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.value.Chat(sid, pid, cid)
	if m.closed || c == nil || c.TitleStatus != "pending" {
		cancel()
		return
	}
	if m.titles == nil {
		m.titles = map[string]titleCall{}
	}
	m.titles[cid] = titleCall{sid: sid, pid: pid, cid: cid, cancel: cancel}
}

func (m *Manager) Detach(op model.Operation) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel := m.cancels[op.Generation]; cancel != nil {
		cancel()
		delete(m.cancels, op.Generation)
	}
	for _, b := range m.value.Sessions {
		if b.Pending != nil && b.Pending.Generation == op.Generation {
			if m.abandoned == nil {
				m.abandoned = map[string]bool{}
			}
			m.abandoned[op.Generation] = true
			break
		}
	}
}
