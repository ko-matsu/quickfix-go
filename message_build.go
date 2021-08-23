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
	IgnoreLogonReset             bool
}

// BuildMessageOutput stores build message output data
type BuildMessageOutput struct {
	MsgBytes     []byte
	Msg          *Message
	SentReset    bool
	SeqNum       int
	ErrorsForLog []error
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
	outputData := BuildMessageOutput{}
	tmpErr := fillDefaultHeader(m.store, msg, bd.InReplyTo, bd.SessionID, bd.EnableLastMsgSeqNumProcessed, bd.TimestampPrecision)
	outputData.AddErrorForLog(tmpErr)
	outputData.SeqNum = m.store.NextSenderMsgSeqNum()
	msg.Header.SetField(tagMsgSeqNum, FIXInt(outputData.SeqNum))
	outputData.Msg = msg
	output = &outputData

	msgType, err := msg.Header.GetBytes(tagMsgType)
	if err != nil {
		return
	}

	if isAdminMessageType(msgType) {
		if bytes.Equal(msgType, msgTypeLogon) {
			var resetSeqNumFlag FIXBoolean
			if msg.Body.Has(tagResetSeqNumFlag) {
				if err = msg.Body.GetField(tagResetSeqNumFlag, &resetSeqNumFlag); err != nil {
					return
				}
			}

			switch {
			case resetSeqNumFlag.Bool() && bd.IgnoreLogonReset:
				outputData.SentReset = true
			case resetSeqNumFlag.Bool():
				if err = m.store.Reset(); err != nil {
					return
				}
				outputData.SentReset = true
				outputData.SeqNum = m.store.NextSenderMsgSeqNum()
				msg.Header.SetField(tagMsgSeqNum, FIXInt(outputData.SeqNum))
			}
		}
	}

	outputData.MsgBytes = msg.build()
	outputData.Msg = msg

	return
}

func (b *BuildMessageOutput) AddErrorForLog(err error) {
	if err == nil {
		return
	}
	if len(b.ErrorsForLog) == 0 {
		b.ErrorsForLog = make([]error, 0, 1)
	}
	b.ErrorsForLog = append(b.ErrorsForLog, err)
}

func (b *BuildMessageOutput) GetErrorsForLog() []error {
	if b == nil {
		return nil
	}
	return b.ErrorsForLog
}

func (b *BuildMessageOutput) IsSentReset() bool {
	if b == nil {
		return false
	}
	return b.SentReset
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
