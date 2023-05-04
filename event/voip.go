// Copyright (c) 2021 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package event

import (
	"encoding/json"
	"fmt"
	"strconv"

	"maunium.net/go/mautrix/id"
)

type CallHangupReason string

const (
	CallHangupICEFailed        CallHangupReason = "ice_failed"
	CallHangupInviteTimeout    CallHangupReason = "invite_timeout"
	CallHangupUserHangup       CallHangupReason = "user_hangup"
	CallHangupUserMediaFailed  CallHangupReason = "user_media_failed"
	CallHangupKeepAliveTimeout CallHangupReason = "keep_alive_timeout"
	CallHangupUnknownError     CallHangupReason = "unknown_error"
)

type CallSDPType string

const (
	CallSDPTypeOffer  CallSDPType = "offer"
	CallSDPTypeAnswer CallSDPType = "answer"
)

type CallCandidate struct {
	Candidate     string `json:"candidate"`
	SDPMLineIndex int    `json:"sdpMLineIndex"`
	SDPMID        string `json:"sdpMid"`
}

type CallVersion string

func (cv *CallVersion) UnmarshalJSON(raw []byte) error {
	var numberVersion int
	err := json.Unmarshal(raw, &numberVersion)
	if err != nil {
		var stringVersion string
		err = json.Unmarshal(raw, &stringVersion)
		if err != nil {
			return fmt.Errorf("failed to parse CallVersion: %w", err)
		}
		*cv = CallVersion(stringVersion)
	} else {
		*cv = CallVersion(strconv.Itoa(numberVersion))
	}
	return nil
}

func (cv *CallVersion) MarshalJSON() ([]byte, error) {
	for _, char := range *cv {
		if char < '0' || char > '9' {
			// The version contains weird characters, return as string.
			return json.Marshal(string(*cv))
		}
	}
	// The version consists of only ASCII digits, return as an integer.
	return []byte(*cv), nil
}

func (cv *CallVersion) Int() (int, error) {
	return strconv.Atoi(string(*cv))
}

type BaseCallEventContent struct {
	CallID          string       `json:"call_id"`
	ConfID          string       `json:"conf_id"`
	PartyID         string       `json:"party_id"`
	Version         CallVersion  `json:"version"`
	DeviceID        id.DeviceID  `json:"device_id"`
	DestSessionID   id.SessionID `json:"dest_session_id"`
	SenderSessionID id.SessionID `json:"sender_session_id"`
}

type CallTrackPurpose string

const (
	CallTrackPurposeUserMedia   CallTrackPurpose = "m.usermedia"
	CallTrackPurposeScreenShare CallTrackPurpose = "m.screenshare"
)

type CallTrack struct {
	Kind    string           `json:"kind,omitempty"`
	Muted   bool             `json:"muted,omitempty"`
	Mid     int              `json:"mid,omitempty"`
	Layers  []CallTrackLayer `json:"layers,omitempty"`
	Purpose CallTrackPurpose `json:"purpose"`
}

type CallTrackLayer struct {
	Width   int              `json:"width,omitempty"`
	Height  int              `json:"height,omitempty"`
	SSRC    int              `json:"ssrc,omitempty"`
	Bitrate int              `json:"bitrate,omitempty"`
	Quality CallTrackQuality `json:"quality,omitempty"`
}

type CallTrackQuality string

const (
	CallTrackQualityOff    CallTrackQuality = "off"
	CallTrackQualityHigh   CallTrackQuality = "high"
	CallTrackQualityMedium CallTrackQuality = "medium"
	CallTrackQualityLow    CallTrackQuality = "low"
)

type CallTrackID string

type CallInviteEventContent CallNegotiateEventContent

type CallCandidatesEventContent struct {
	BaseCallEventContent
	Candidates []CallCandidate `json:"candidates"`
}

type CallSelectAnswerEventContent struct {
	BaseCallEventContent
	SelectedPartyID string `json:"selected_party_id"`
}

type CallNegotiateEventContent struct {
	BaseCallEventContent
	Lifetime  int               `json:"lifetime"`
	Negotiate CallNegotiateBody `json:"negotiate"`
}

type CallNegotiateBody struct {
	SDP    string                    `json:"sdp"`
	Type   CallSDPType               `json:"type"`
	Tracks map[CallTrackID]CallTrack `json:"tracks"`
}

type CallHangupEventContent struct {
	BaseCallEventContent
	Reason CallHangupReason `json:"reason"`
}

type CallSubscriptionEventContent struct {
	BaseCallEventContent
	Subscribe   []CallTrackRequest `json:"subscribe"`
	Unsubscribe []CallTrackRequest `json:"unsubscribe"`
}

type CallTrackRequest struct {
	TrackID CallTrackID `json:"track_id"`
	Width   int         `json:"width,omitempty"`
	Height  int         `json:"height,omitempty"`
}

type CallTrackOwner struct {
	UserID   id.UserID   `json:"user_id"`
	DeviceID id.DeviceID `json:"device_id"`
}

type CallTrackAdvertiseEventContent struct {
	BaseCallEventContent
	UserTracks map[CallTrackOwner]map[CallTrackID]CallTrack `json:"tracks"`
}

type CallTrackUpdateEventContent struct {
	BaseCallEventContent
	Tracks map[CallTrackID]CallTrack `json:"tracks"`
}

type CallPingEventContent struct{}
type CallPongEventContent struct{}
