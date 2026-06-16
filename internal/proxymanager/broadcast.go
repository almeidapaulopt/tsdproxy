// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"sync"

	"github.com/almeidapaulopt/tsdproxy/internal/core/webhook"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

const statusSubChanBuf = 64

type statusSubscription struct {
	ch   chan model.ProxyEvent
	once sync.Once
}

// SubscribeStatusEvents returns a channel of proxy events and a cancel function.
func (pm *ProxyManager) SubscribeStatusEvents() (<-chan model.ProxyEvent, func()) {
	sub := &statusSubscription{ch: make(chan model.ProxyEvent, statusSubChanBuf)}

	pm.subMu.Lock()
	pm.statusSubscribers[sub] = struct{}{}
	pm.subMu.Unlock()

	cancel := func() {
		sub.once.Do(func() {
			pm.subMu.Lock()
			delete(pm.statusSubscribers, sub)
			close(sub.ch)
			pm.subMu.Unlock()
		})
	}

	return sub.ch, cancel
}

// broadcastStatusEvents broadcasts proxy status event to all SubscribeStatusEvents
func (pm *ProxyManager) broadcastStatusEvents(event model.ProxyEvent) {
	if pm.webhookSender != nil {
		pm.webhookSender.Send(webhook.NewEvent(event.ID, event.OldStatus, event.Status))
	}

	pm.subMu.RLock()
	for sub := range pm.statusSubscribers {
		select {
		case sub.ch <- event:
		default:
			pm.log.Warn().
				Str("proxy", event.ID).
				Str("status", event.Status.String()).
				Msg("status event dropped: subscriber channel full (slow consumer)")
		}
	}
	pm.subMu.RUnlock()
}
