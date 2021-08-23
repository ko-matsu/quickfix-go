package quickfix

import (
	"bytes"
	"errors"
	"time"
)

// BuildMessageInput stores for building message data
type BuildMessageInput struct {
	Msg                          *Message
	InReplyTo                    *Message
	SessionID                    SessionID
	EnableLastMsgSeqNumProcessed bool
	TimestampPrecision           TimestampPrecision
	application                  Application
	logger                       Log
	IgnoreLogonReset             bool
}

// BuildMessageOutput stores build message output data
type BuildMessageOutput struct {
	MsgBytes  []byte
	Msg       *Message
	SentReset bool
	SeqNum    int
}

type MsgSeqNumCursor interface {
	NextSenderMsgSeqNum() int
	NextTargetMsgSeqNum() int

	Reset() error
}

type messageBuilder struct {
	store MsgSeqNumCursor
}

func newMessageBuilder(store MsgSeqNumCursor) *messageBuilder {
	return &messageBuilder{store}
}

func (m *messageBuilder) BuildMessage(bd *BuildMessageInput) (output *BuildMessageOutput, err error) {
	if m == nil || m.store == nil {
		err = errors.New("failed to initialize. please to set store")
		return
	}
	msg := bd.Msg
	tmpErr := fillDefaultHeader(m.store, msg, bd.InReplyTo, bd.SessionID, bd.EnableLastMsgSeqNumProcessed, bd.TimestampPrecision)
	if tmpErr != nil && bd.logger != nil {
		bd.logger.OnEvent(err.Error())
	}
	outputData := BuildMessageOutput{}
	outputData.SeqNum = m.store.NextSenderMsgSeqNum()
	msg.Header.SetField(tagMsgSeqNum, FIXInt(outputData.SeqNum))

	msgType, err := msg.Header.GetBytes(tagMsgType)
	if err != nil {
		return
	}

	if isAdminMessageType(msgType) {
		if bd.application != nil {
			bd.application.ToAdmin(msg, bd.SessionID)
		}

		if bytes.Equal(msgType, msgTypeLogon) {
			var resetSeqNumFlag FIXBoolean
			if msg.Body.Has(tagResetSeqNumFlag) {
				if err = msg.Body.GetField(tagResetSeqNumFlag, &resetSeqNumFlag); err != nil {
					return
				}
			}

			if !resetSeqNumFlag.Bool() {
				// do nothing
			} else if bd.IgnoreLogonReset {
				outputData.SentReset = true
			} else {
				if err = m.store.Reset(); err != nil {
					return
				}

				outputData.SentReset = true
				outputData.SeqNum = m.store.NextSenderMsgSeqNum()
				msg.Header.SetField(tagMsgSeqNum, FIXInt(outputData.SeqNum))
			}
		}
	} else if bd.application != nil {
		if err = bd.application.ToApp(msg, bd.SessionID); err != nil {
			return
		}
	}

	outputData.MsgBytes = msg.build()
	outputData.Msg = msg
	output = &outputData

	return
}

func insertSendingTime(msg *Message, sessionID SessionID, timestampPrecision TimestampPrecision) {
	sendingTime := time.Now().UTC()

	if sessionID.BeginString >= BeginStringFIX42 {
		msg.Header.SetField(tagSendingTime, FIXUTCTimestamp{Time: sendingTime, Precision: timestampPrecision})
	} else {
		msg.Header.SetField(tagSendingTime, FIXUTCTimestamp{Time: sendingTime, Precision: Seconds})
	}
}

func fillDefaultHeader(store MsgSeqNumCursor, msg *Message, inReplyTo *Message, sessionID SessionID, enableLastMsgSeqNumProcessed bool, timestampPrecision TimestampPrecision) error {
	msg.Header.SetString(tagBeginString, sessionID.BeginString)
	msg.Header.SetString(tagSenderCompID, sessionID.SenderCompID)
	optionallySetID(msg, tagSenderSubID, sessionID.SenderSubID)
	optionallySetID(msg, tagSenderLocationID, sessionID.SenderLocationID)

	msg.Header.SetString(tagTargetCompID, sessionID.TargetCompID)
	optionallySetID(msg, tagTargetSubID, sessionID.TargetSubID)
	optionallySetID(msg, tagTargetLocationID, sessionID.TargetLocationID)

	insertSendingTime(msg, sessionID, timestampPrecision)

	if enableLastMsgSeqNumProcessed {
		if inReplyTo != nil {
			lastSeqNum, err := inReplyTo.Header.GetInt(tagMsgSeqNum)
			if err != nil {
				return err
			}
			msg.Header.SetInt(tagLastMsgSeqNumProcessed, lastSeqNum)
		} else {
			msg.Header.SetInt(tagLastMsgSeqNumProcessed, store.NextTargetMsgSeqNum()-1)
		}
	}
	return nil
}
