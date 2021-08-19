package quickfix

import (
	"fmt"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// FileStoreTestSuite runs all tests in the MessageStoreTestSuite against the FileStore implementation
type FileStoreTestSuite struct {
	MessageStoreTestSuite
}

func (suite *FileStoreTestSuite) SetupTest() {
	testName := suite.getTestName(suite.T())
	fileStorePath := path.Join(suite.T().TempDir(), fmt.Sprintf("FileStoreTestSuite-%s-%d", testName, time.Now().UnixNano()))
	sessionID := SessionID{BeginString: "FIX.4.4", SenderCompID: "SENDER", TargetCompID: "TARGET"}

	// create settings
	settings, err := ParseSettings(strings.NewReader(fmt.Sprintf(`
[DEFAULT]
FileStorePath=%s

[SESSION]
BeginString=%s
SenderCompID=%s
TargetCompID=%s`, fileStorePath, sessionID.BeginString, sessionID.SenderCompID, sessionID.TargetCompID)))
	require.Nil(suite.T(), err)

	// create store
	suite.msgStore, err = NewFileStoreFactory(settings).Create(sessionID)
	require.Nil(suite.T(), err)
}

func (suite *FileStoreTestSuite) TearDownTest() {
	suite.msgStore.Close()
	// os.RemoveAll(suite.getDirPath(suite.T()))
}

func TestFileStoreTestSuite(t *testing.T) {
	suite.Run(t, new(FileStoreTestSuite))
}
