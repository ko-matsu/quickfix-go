package quickfix

import (
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
)

type sqlTxStoreFactory struct {
	sqlStoreFactory
}

type sqlTxStore struct {
	sqlStore
}

// NewSQLTxStoreFactory returns a sql-based implementation of MessageStoreFactory
func NewSQLTxStoreFactory(settings *Settings) MessageStoreFactory {
	return sqlTxStoreFactory{sqlStoreFactory{settings: settings}}
}

// Create creates a new SQLStore implementation of the MessageStore interface
func (f sqlTxStoreFactory) Create(sessionID SessionID) (msgStore MessageStore, err error) {
	sqlMsg, err := f.create(sessionID)
	if err != nil {
		return
	}
	sqlTxStore := sqlTxStore{
		sqlStore: *sqlMsg,
	}
	err = sqlTxStore.open()
	if err != nil {
		return
	}
	msgStore = &sqlTxStore
	return
}

func (store *sqlTxStore) BuildAndSaveMessage(
	messageBuildData *MessageBuildData,
	buildFunc func(MessageStore, *MessageBuildData, interface{}) (*MessageBuildOutputData, error),
) (output *MessageBuildOutputData, err error) {
	if store.db == nil {
		return nil, ErrAccessToClosedStore
	}
	s := store.sessionID

	err = store.db.Transaction(func(tx *gorm.DB) error {
		var outgoingSeqNum int
		row := tx.Raw(`SELECT outgoing_seqnum FROM sessions
			WHERE beginstring = ? AND session_qualifier = ?
			AND sendercompid = ? AND sendersubid = ? AND senderlocid = ?
			AND targetcompid = ? AND targetsubid = ? AND targetlocid = ?`,
			s.BeginString, s.Qualifier,
			s.SenderCompID, s.SenderSubID, s.SenderLocationID,
			s.TargetCompID, s.TargetSubID, s.TargetLocationID).Row()

		if err := row.Scan(&outgoingSeqNum); err != nil {
			return err
		}
		if outgoingSeqNum != store.cache.NextSenderMsgSeqNum() {
			store.cache.SetNextSenderMsgSeqNum(outgoingSeqNum) // refresh
		}

		outputData, err := buildFunc(store, messageBuildData, tx)
		if err != nil {
			return err
		}
		output = outputData // Response should also be returned in case of an error.
		seqNum := store.cache.NextSenderMsgSeqNum()
		if seqNum != output.SeqNum {
			return errors.New("Internal error: unmatch seqnum")
		}
		nextSeqNum := seqNum + 1

		if err := tx.Exec(`INSERT INTO messages (
			msgseqnum, message,
			beginstring, session_qualifier,
			sendercompid, sendersubid, senderlocid,
			targetcompid, targetsubid, targetlocid)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			seqNum, string(outputData.MsgBytes),
			s.BeginString, s.Qualifier,
			s.SenderCompID, s.SenderSubID, s.SenderLocationID,
			s.TargetCompID, s.TargetSubID, s.TargetLocationID).Error; err != nil {
			return err
		}

		if err := tx.Exec(`UPDATE sessions SET outgoing_seqnum = ?
			WHERE beginstring = ? AND session_qualifier = ?
			AND sendercompid = ? AND sendersubid = ? AND senderlocid = ?
			AND targetcompid = ? AND targetsubid = ? AND targetlocid = ?`,
			nextSeqNum, s.BeginString, s.Qualifier,
			s.SenderCompID, s.SenderSubID, s.SenderLocationID,
			s.TargetCompID, s.TargetSubID, s.TargetLocationID).Error; err != nil {
			return err
		}
		return store.cache.SetNextSenderMsgSeqNum(nextSeqNum)
	})
	if err != nil {
		// Response should also be returned in case of an error.
		return
	}
	return output, nil
}

func (store *sqlTxStore) ResetByTx(tx interface{}) (err error) {
	if store.db == nil {
		return ErrAccessToClosedStore
	}
	dbTx, ok := tx.(*gorm.DB)
	if !ok {
		return errors.New("invalid transaction")
	}

	s := store.sessionID
	if err = dbTx.Exec(`DELETE FROM messages
		WHERE beginstring = ? AND session_qualifier = ?
		AND sendercompid = ? AND sendersubid = ? AND senderlocid = ?
		AND targetcompid = ? AND targetsubid = ? AND targetlocid = ?`,
		s.BeginString, s.Qualifier,
		s.SenderCompID, s.SenderSubID, s.SenderLocationID,
		s.TargetCompID, s.TargetSubID, s.TargetLocationID).Error; err != nil {
		return err
	}

	if err = store.cache.Reset(); err != nil {
		return err
	}

	return dbTx.Exec(`UPDATE sessions
		SET creation_time = ?, incoming_seqnum = ?, outgoing_seqnum = ?
		WHERE beginstring = ? AND session_qualifier = ?
		AND sendercompid= ? AND sendersubid = ? AND senderlocid = ?
		AND targetcompid = ? AND targetsubid = ? AND targetlocid = ?`,
		store.cache.CreationTime(), store.cache.NextTargetMsgSeqNum(), store.cache.NextSenderMsgSeqNum(),
		s.BeginString, s.Qualifier,
		s.SenderCompID, s.SenderSubID, s.SenderLocationID,
		s.TargetCompID, s.TargetSubID, s.TargetLocationID).Error
}
