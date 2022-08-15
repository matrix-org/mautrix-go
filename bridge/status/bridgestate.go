// Copyright (c) 2022 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package status

import (
	"time"

	"maunium.net/go/mautrix/id"
)

type BridgeStateEvent string
type BridgeStateErrorCode string

type BridgeStateErrorMap map[BridgeStateErrorCode]string

func (bem BridgeStateErrorMap) Update(data BridgeStateErrorMap) {
	for key, value := range data {
		bem[key] = value
	}
}

var BridgeStateHumanErrors = make(BridgeStateErrorMap)

const (
	StateUnconfigured        BridgeStateEvent = "UNCONFIGURED"
	StateRunning             BridgeStateEvent = "RUNNING"
	StateConnecting          BridgeStateEvent = "CONNECTING"
	StateBackfilling         BridgeStateEvent = "BACKFILLING"
	StateConnected           BridgeStateEvent = "CONNECTED"
	StateTransientDisconnect BridgeStateEvent = "TRANSIENT_DISCONNECT"
	StateBadCredentials      BridgeStateEvent = "BAD_CREDENTIALS"
	StateUnknownError        BridgeStateEvent = "UNKNOWN_ERROR"
	StateLoggedOut           BridgeStateEvent = "LOGGED_OUT"
)

type BridgeState struct {
	StateEvent BridgeStateEvent `json:"state_event"`
	Timestamp  int64            `json:"timestamp"`
	TTL        int              `json:"ttl"`

	Source  string               `json:"source,omitempty"`
	Error   BridgeStateErrorCode `json:"error,omitempty"`
	Message string               `json:"message,omitempty"`

	UserID     id.UserID `json:"user_id,omitempty"`
	RemoteID   string    `json:"remote_id,omitempty"`
	RemoteName string    `json:"remote_name,omitempty"`

	Reason string                 `json:"reason,omitempty"`
	Info   map[string]interface{} `json:"info,omitempty"`
}

type GlobalBridgeState struct {
	RemoteStates map[string]BridgeState `json:"remoteState"`
	BridgeState  BridgeState            `json:"bridgeState"`
}

type BridgeStateFiller interface {
	GetMXID() id.UserID
	GetRemoteID() string
	GetRemoteName() string
}

func (pong BridgeState) Fill(user BridgeStateFiller) BridgeState {
	if user != nil {
		pong.UserID = user.GetMXID()
		pong.RemoteID = user.GetRemoteID()
		pong.RemoteName = user.GetRemoteName()
	}

	pong.Timestamp = time.Now().Unix()
	pong.Source = "bridge"
	if len(pong.Error) > 0 {
		pong.TTL = 60
		msg, ok := BridgeStateHumanErrors[pong.Error]
		if ok {
			pong.Message = msg
		}
	} else {
		pong.TTL = 240
	}
	return pong
}

func (pong *BridgeState) ShouldDeduplicate(newPong *BridgeState) bool {
	if pong == nil || pong.StateEvent != newPong.StateEvent || pong.Error != newPong.Error {
		return false
	}
	return pong.Timestamp+int64(pong.TTL/5) > time.Now().Unix()
}
