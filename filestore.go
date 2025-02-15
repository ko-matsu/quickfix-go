package quickfix

import (
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/cryptogarageinc/quickfix-go/config"
	"github.com/pkg/errors"
)

type msgDef struct {
	offset int64
	size   int
}

type fileStoreFactory struct {
	settings *Settings
}

type fileStore struct {
	sessionID          SessionID
	cache              *memoryStore
	offsets            map[int]msgDef
	bodyFname          string
	headerFname        string
	sessionFname       string
	senderSeqNumsFname string
	targetSeqNumsFname string
	bodyFile           *os.File
	headerFile         *os.File
	sessionFile        *os.File
	senderSeqNumsFile  *os.File
	targetSeqNumsFile  *os.File

	isClosed bool
	*messageBuilder
}

// NewFileStoreFactory returns a file-based implementation of MessageStoreFactory
func NewFileStoreFactory(settings *Settings) MessageStoreFactory {
	return fileStoreFactory{settings: settings}
}

// Create creates a new FileStore implementation of the MessageStore interface
func (f fileStoreFactory) Create(sessionID SessionID) (msgStore MessageStore, err error) {
	var dirname string

	settings := make([]*SessionSettings, 1, 2)
	settings[0] = f.settings.GlobalSettings()
	if sessionSettings, ok := f.settings.SessionSettings()[sessionID]; ok {
		settings = append(settings, sessionSettings)
	}
	for _, sessionSettings := range settings {
		if sessionSettings.HasSetting(config.FileStorePath) {
			dirname, err = sessionSettings.Setting(config.FileStorePath)
			if err != nil {
				return nil, err
			}
		}
	}

	if len(dirname) == 0 {
		return nil, fmt.Errorf("FileStorePath configuration is not found. session: %v", sessionID)
	}
	return newFileStore(sessionID, dirname)
}

func newFileStore(sessionID SessionID, dirname string) (*fileStore, error) {
	if err := os.MkdirAll(dirname, os.ModePerm); err != nil {
		return nil, err
	}

	sessionPrefix := sessionIDFilenamePrefix(sessionID)

	store := &fileStore{
		sessionID:          sessionID,
		cache:              &memoryStore{},
		offsets:            make(map[int]msgDef),
		bodyFname:          path.Join(dirname, fmt.Sprintf("%s.%s", sessionPrefix, "body")),
		headerFname:        path.Join(dirname, fmt.Sprintf("%s.%s", sessionPrefix, "header")),
		sessionFname:       path.Join(dirname, fmt.Sprintf("%s.%s", sessionPrefix, "session")),
		senderSeqNumsFname: path.Join(dirname, fmt.Sprintf("%s.%s", sessionPrefix, "senderseqnums")),
		targetSeqNumsFname: path.Join(dirname, fmt.Sprintf("%s.%s", sessionPrefix, "targetseqnums")),
	}
	store.messageBuilder = newMessageBuilder(store)

	if err := store.Refresh(); err != nil {
		return nil, err
	}

	return store, nil
}

// Reset deletes the store files and sets the seqnums back to 1
func (store *fileStore) Reset() error {
	if store.isClosed {
		return ErrAccessToClosedStore
	}
	if err := store.cache.Reset(); err != nil {
		return errors.Wrap(err, "cache reset")
	}

	if err := store.close(); err != nil {
		return errors.Wrap(err, "close")
	}
	if err := removeFile(store.bodyFname); err != nil {
		return err
	}
	if err := removeFile(store.headerFname); err != nil {
		return err
	}
	if err := removeFile(store.sessionFname); err != nil {
		return err
	}
	if err := removeFile(store.senderSeqNumsFname); err != nil {
		return err
	}
	if err := removeFile(store.targetSeqNumsFname); err != nil {
		return err
	}
	return store.Refresh()
}

// Refresh closes the store files and then reloads from them
func (store *fileStore) Refresh() (err error) {
	if store.isClosed {
		return ErrAccessToClosedStore
	}
	if err = store.cache.Reset(); err != nil {
		err = errors.Wrap(err, "cache reset")
		return
	}

	if err = store.close(); err != nil {
		return err
	}

	creationTimePopulated, err := store.populateCache()
	if err != nil {
		return err
	}

	if store.bodyFile, err = openOrCreateFile(store.bodyFname, 0660); err != nil {
		return err
	}
	if store.headerFile, err = openOrCreateFile(store.headerFname, 0660); err != nil {
		return err
	}
	if store.sessionFile, err = openOrCreateFile(store.sessionFname, 0660); err != nil {
		return err
	}
	if store.senderSeqNumsFile, err = openOrCreateFile(store.senderSeqNumsFname, 0660); err != nil {
		return err
	}
	if store.targetSeqNumsFile, err = openOrCreateFile(store.targetSeqNumsFname, 0660); err != nil {
		return err
	}

	if !creationTimePopulated {
		if err := store.setSession(); err != nil {
			return err
		}
	}

	if err := store.SetNextSenderMsgSeqNum(store.NextSenderMsgSeqNum()); err != nil {
		return errors.Wrap(err, "set next sender")
	}

	if err := store.SetNextTargetMsgSeqNum(store.NextTargetMsgSeqNum()); err != nil {
		return errors.Wrap(err, "set next target")
	}
	return nil
}

func (store *fileStore) populateCache() (creationTimePopulated bool, err error) {
	if tmpHeaderFile, err := os.Open(store.headerFname); err == nil {
		defer tmpHeaderFile.Close()
		for {
			var seqNum, size int
			var offset int64
			if cnt, err := fmt.Fscanf(tmpHeaderFile, "%d,%d,%d\n", &seqNum, &offset, &size); err != nil || cnt != 3 {
				break
			}
			store.offsets[seqNum] = msgDef{offset: offset, size: size}
		}
	}

	if timeBytes, err := os.ReadFile(store.sessionFname); err == nil {
		var ctime time.Time
		if err := ctime.UnmarshalText(timeBytes); err == nil {
			store.cache.creationTime = ctime
			creationTimePopulated = true
		}
	}

	if senderSeqNumBytes, err := os.ReadFile(store.senderSeqNumsFname); err == nil {
		if senderSeqNum, err := strconv.Atoi(string(senderSeqNumBytes)); err == nil {
			if err = store.cache.SetNextSenderMsgSeqNum(senderSeqNum); err != nil {
				return creationTimePopulated, errors.Wrap(err, "cache set next sender")
			}
		}
	}

	if targetSeqNumBytes, err := os.ReadFile(store.targetSeqNumsFname); err == nil {
		if targetSeqNum, err := strconv.Atoi(string(targetSeqNumBytes)); err == nil {
			if err = store.cache.SetNextTargetMsgSeqNum(targetSeqNum); err != nil {
				return creationTimePopulated, errors.Wrap(err, "cache set next target")
			}
		}
	}

	return creationTimePopulated, nil
}

func (store *fileStore) setSession() error {
	if store.isClosed || store.sessionFile == nil {
		return ErrAccessToClosedStore
	}
	if _, err := store.sessionFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("unable to rewind file: %s: %s", store.sessionFname, err.Error())
	}

	data, err := store.cache.CreationTime().MarshalText()
	if err != nil {
		return fmt.Errorf("unable to marshal session time to file: %s: %s", store.sessionFname, err.Error())
	}
	if _, err := store.sessionFile.Write(data); err != nil {
		return fmt.Errorf("unable to write to file: %s: %s", store.sessionFname, err.Error())
	}
	if err := store.sessionFile.Sync(); err != nil {
		return fmt.Errorf("unable to flush file: %s: %s", store.sessionFname, err.Error())
	}
	return nil
}

func (store *fileStore) setSeqNum(f *os.File, seqNum int) error {
	if store.isClosed || f == nil {
		return ErrAccessToClosedStore
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("unable to rewind file: %s: %s", f.Name(), err.Error())
	}
	if _, err := fmt.Fprintf(f, "%019d", seqNum); err != nil {
		return fmt.Errorf("unable to write to file: %s: %s", f.Name(), err.Error())
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("unable to flush file: %s: %s", f.Name(), err.Error())
	}
	return nil
}

// NextSenderMsgSeqNum returns the next MsgSeqNum that will be sent
func (store *fileStore) NextSenderMsgSeqNum() int {
	return store.cache.NextSenderMsgSeqNum()
}

// NextTargetMsgSeqNum returns the next MsgSeqNum that should be received
func (store *fileStore) NextTargetMsgSeqNum() int {
	return store.cache.NextTargetMsgSeqNum()
}

// SetNextSenderMsgSeqNum sets the next MsgSeqNum that will be sent
func (store *fileStore) SetNextSenderMsgSeqNum(next int) error {
	if err := store.cache.SetNextSenderMsgSeqNum(next); err != nil {
		return errors.Wrap(err, "cache")
	}
	return store.setSeqNum(store.senderSeqNumsFile, next)
}

// SetNextTargetMsgSeqNum sets the next MsgSeqNum that should be received
func (store *fileStore) SetNextTargetMsgSeqNum(next int) error {
	if err := store.cache.SetNextTargetMsgSeqNum(next); err != nil {
		return errors.Wrap(err, "cache")
	}
	return store.setSeqNum(store.targetSeqNumsFile, next)
}

// IncrNextSenderMsgSeqNum increments the next MsgSeqNum that will be sent
func (store *fileStore) IncrNextSenderMsgSeqNum() error {
	if err := store.cache.IncrNextSenderMsgSeqNum(); err != nil {
		return errors.Wrap(err, "cache")
	}
	return store.setSeqNum(store.senderSeqNumsFile, store.cache.NextSenderMsgSeqNum())
}

// IncrNextTargetMsgSeqNum increments the next MsgSeqNum that should be received
func (store *fileStore) IncrNextTargetMsgSeqNum() error {
	if err := store.cache.IncrNextTargetMsgSeqNum(); err != nil {
		return errors.Wrap(err, "cache")
	}
	return store.setSeqNum(store.targetSeqNumsFile, store.cache.NextTargetMsgSeqNum())
}

// CreationTime returns the creation time of the store
func (store *fileStore) CreationTime() time.Time {
	return store.cache.CreationTime()
}

func (store *fileStore) SaveMessage(seqNum int, msg []byte) error {
	if store.isClosed || store.bodyFile == nil {
		return ErrAccessToClosedStore
	}
	offset, err := store.bodyFile.Seek(0, os.SEEK_END)
	if err != nil {
		return fmt.Errorf("unable to seek to end of file: %s: %s", store.bodyFname, err.Error())
	}
	if _, err := store.headerFile.Seek(0, os.SEEK_END); err != nil {
		return fmt.Errorf("unable to seek to end of file: %s: %s", store.headerFname, err.Error())
	}
	if _, err := fmt.Fprintf(store.headerFile, "%d,%d,%d\n", seqNum, offset, len(msg)); err != nil {
		return fmt.Errorf("unable to write to file: %s: %s", store.headerFname, err.Error())
	}

	store.offsets[seqNum] = msgDef{offset: offset, size: len(msg)}

	if _, err := store.bodyFile.Write(msg); err != nil {
		return fmt.Errorf("unable to write to file: %s: %s", store.bodyFname, err.Error())
	}
	if err := store.bodyFile.Sync(); err != nil {
		return fmt.Errorf("unable to flush file: %s: %s", store.bodyFname, err.Error())
	}
	if err := store.headerFile.Sync(); err != nil {
		return fmt.Errorf("unable to flush file: %s: %s", store.headerFname, err.Error())
	}
	return nil
}

func (store *fileStore) getMessage(seqNum int) (msg []byte, found bool, err error) {
	if store.isClosed || store.bodyFile == nil {
		err = ErrAccessToClosedStore
		return
	}
	msgInfo, found := store.offsets[seqNum]
	if !found {
		return
	}

	msg = make([]byte, msgInfo.size)
	if _, err = store.bodyFile.ReadAt(msg, msgInfo.offset); err != nil {
		return nil, true, fmt.Errorf("unable to read from file: %s: %s", store.bodyFname, err.Error())
	}

	return msg, true, nil
}

func (store *fileStore) GetMessages(beginSeqNum, endSeqNum int) ([][]byte, error) {
	var msgs [][]byte
	for seqNum := beginSeqNum; seqNum <= endSeqNum; seqNum++ {
		m, found, err := store.getMessage(seqNum)
		if err != nil {
			return nil, err
		}
		if found {
			msgs = append(msgs, m)
		}
	}
	return msgs, nil
}

func (store *fileStore) SaveMessageWithTx(messageBuildData *BuildMessageInput) (output *BuildMessageOutput, err error) {
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

// Close closes the store's files
func (store *fileStore) Close() error {
	store.isClosed = true
	return store.close()
}

func (store *fileStore) close() error {
	if err := closeFile(store.bodyFile); err != nil {
		return err
	}
	if err := closeFile(store.headerFile); err != nil {
		return err
	}
	if err := closeFile(store.sessionFile); err != nil {
		return err
	}
	if err := closeFile(store.senderSeqNumsFile); err != nil {
		return err
	}
	if err := closeFile(store.targetSeqNumsFile); err != nil {
		return err
	}

	store.bodyFile = nil
	store.headerFile = nil
	store.sessionFile = nil
	store.senderSeqNumsFile = nil
	store.targetSeqNumsFile = nil

	return nil
}
