// Copyright (c) 2022 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build cgo && !nocrypto

package bridge

import (
	"database/sql"
	"errors"

	"github.com/lib/pq"

	"maunium.net/go/mautrix/crypto"
	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/util/dbutil"
)

func init() {
	crypto.PostgresArrayWrapper = pq.Array
}

type SQLCryptoStore struct {
	*crypto.SQLCryptoStore
	UserID        id.UserID
	GhostIDFormat string
}

var _ crypto.Store = (*SQLCryptoStore)(nil)

func NewSQLCryptoStore(db *dbutil.Database, userID id.UserID, ghostIDFormat, pickleKey string) *SQLCryptoStore {
	return &SQLCryptoStore{
		SQLCryptoStore: crypto.NewSQLCryptoStore(db, "", "", []byte(pickleKey)),
		UserID:         userID,
		GhostIDFormat:  ghostIDFormat,
	}
}

func (store *SQLCryptoStore) FindDeviceID() (deviceID id.DeviceID) {
	err := store.DB.QueryRow("SELECT device_id FROM crypto_account WHERE account_id=$1", store.AccountID).Scan(&deviceID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		store.Log.Warn("Failed to scan device ID: %v", err)
	}
	return
}

func (store *SQLCryptoStore) GetRoomMembers(roomID id.RoomID) (members []id.UserID, err error) {
	var rows *sql.Rows
	rows, err = store.DB.Query(`
		SELECT user_id FROM mx_user_profile
		WHERE room_id=$1
			AND (membership='join' OR membership='invite')
			AND user_id<>$2
			AND user_id NOT LIKE $3
	`, roomID, store.UserID, store.GhostIDFormat)
	if err != nil {
		return
	}
	for rows.Next() {
		var userID id.UserID
		err = rows.Scan(&userID)
		if err != nil {
			store.Log.Warn("Failed to scan member in %s: %v", roomID, err)
		} else {
			members = append(members, userID)
		}
	}
	return
}
