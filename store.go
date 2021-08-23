package quickfix

import (
	"time"

	"github.com/pkg/errors"
)

//The MessageStore interface provides methods to record and retrieve messages for resend purposes
type MessageStore interface {
	NextSenderMsgSeqNum() int
	NextTargetMsgSeqNum() int

	IncrNextSenderMsgSeqNum() error
	IncrNextTargetMsgSeqNum() error

	SetNextSenderMsgSeqNum(next int) error
	SetNextTargetMsgSeqNum(next int) error

	CreationTime() time.Time

	SaveMessage(seqNum int, msg []byte) error
	GetMessages(beginSeqNum, endSeqNum int) ([][]byte, error)

	Refresh() error
	Reset() error

	Close() error

	SaveMessageWithTx(messageBuildData *MessageBuildData) (output *MessageBuildOutputData, err error)
	BuildMessage(messageBuildData *MessageBuildData) (output *MessageBuildOutputData, err error)
}

// MessageBuildData stores for building message data
type MessageBuildData struct {
	Msg                          *Message
	InReplyTo                    *Message
	SessionID                    SessionID
	EnableLastMsgSeqNumProcessed bool
	TimestampPrecision           TimestampPrecision
	application                  *Application
	logger                       *Log
}

// MessageBuildOutputData stores build message output data
type MessageBuildOutputData struct {
	MsgBytes  []byte
	SentReset bool
	SeqNum    int
}

//The MessageStoreFactory interface is used by session to create a session specific message store
type MessageStoreFactory interface {
	Create(sessionID SessionID) (MessageStore, error)
}

type memoryStore struct {
	senderMsgSeqNum, targetMsgSeqNum int
	creationTime                     time.Time
	messageMap                       map[int][]byte

	isClosed bool
}

func (store *memoryStore) NextSenderMsgSeqNum() int {
	return store.senderMsgSeqNum + 1
}

func (store *memoryStore) NextTargetMsgSeqNum() int {
	return store.targetMsgSeqNum + 1
}

func (store *memoryStore) IncrNextSenderMsgSeqNum() error {
	store.senderMsgSeqNum++
	return nil
}

func (store *memoryStore) IncrNextTargetMsgSeqNum() error {
	store.targetMsgSeqNum++
	return nil
}

func (store *memoryStore) SetNextSenderMsgSeqNum(nextSeqNum int) error {
	if store.isClosed {
		return ErrAccessToClosedStore
	}
	store.senderMsgSeqNum = nextSeqNum - 1
	return nil
}
func (store *memoryStore) SetNextTargetMsgSeqNum(nextSeqNum int) error {
	if store.isClosed {
		return ErrAccessToClosedStore
	}
	store.targetMsgSeqNum = nextSeqNum - 1
	return nil
}

func (store *memoryStore) CreationTime() time.Time {
	return store.creationTime
}

func (store *memoryStore) Reset() error {
	if store.isClosed {
		return ErrAccessToClosedStore
	}
	store.senderMsgSeqNum = 0
	store.targetMsgSeqNum = 0
	store.creationTime = time.Now()
	store.messageMap = nil
	return nil
}

func (store *memoryStore) Refresh() error {
	if store.isClosed {
		return ErrAccessToClosedStore
	}
	return nil
}

func (store *memoryStore) Close() error {
	store.isClosed = true
	return nil
}

func (store *memoryStore) SaveMessage(seqNum int, msg []byte) error {
	if store.isClosed {
		return ErrAccessToClosedStore
	}
	if store.messageMap == nil {
		store.messageMap = make(map[int][]byte)
	}

	store.messageMap[seqNum] = msg
	return nil
}

func (store *memoryStore) GetMessages(beginSeqNum, endSeqNum int) ([][]byte, error) {
	if store.isClosed {
		return nil, ErrAccessToClosedStore
	}
	var msgs [][]byte
	for seqNum := beginSeqNum; seqNum <= endSeqNum; seqNum++ {
		if m, ok := store.messageMap[seqNum]; ok {
			msgs = append(msgs, m)
		}
	}
	return msgs, nil
}

func (store *memoryStore) SaveMessageWithTx(messageBuildData *MessageBuildData) (output *MessageBuildOutputData, err error) {
	output, err = store.BuildMessage(messageBuildData)
	if err != nil {
		return
	}
	err = store.IncrNextSenderMsgSeqNum()
	if err != nil {
		return
	}

	err = store.SaveMessage(output.SeqNum, output.MsgBytes)
	return
}

func (store *memoryStore) BuildMessage(messageBuildData *MessageBuildData) (output *MessageBuildOutputData, err error) {
	return BuildMessageDefault(store, messageBuildData)
}

type memoryStoreFactory struct{}

func (f memoryStoreFactory) Create(sessionID SessionID) (MessageStore, error) {
	m := new(memoryStore)
	if err := m.Reset(); err != nil {
		return m, errors.Wrap(err, "reset")
	}
	return m, nil
}

//NewMemoryStoreFactory returns a MessageStoreFactory instance that created in-memory MessageStores
func NewMemoryStoreFactory() MessageStoreFactory { return memoryStoreFactory{} }

// append API ------------------------------------------------------------------

// ErrAccessToClosedStore defines error on accessing to closed message store.
var ErrAccessToClosedStore = errors.New("this store is already closed")

// BuildMessageDefault returns builded message.
func BuildMessageDefault(store MessageStore, messageBuildData *MessageBuildData) (output *MessageBuildOutputData, err error) {
	return buildMessage(store, messageBuildData)
}
