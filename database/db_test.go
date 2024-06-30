package database

import (
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

const (
	testDbPath = "./testDb.db"
)

func dropDatabase(fileName string) {
	_ = os.Remove(fileName)
}

func clearDb() {
	dropDatabase(testDbPath)
}

func connectDb(t *testing.T) *OneTimeShareDb {
	assert := require.New(t)
	db, err := ConnectDb(testDbPath)

	if err != nil {
		assert.Fail("Problem with creation db connection:" + err.Error())
		return nil
	}
	return db
}

func createDbAndConnect(t *testing.T, assert *require.Assertions) *OneTimeShareDb {
	clearDb()
	return connectDb(t)
}

func TestConnection(t *testing.T) {
	assert := require.New(t)
	dropDatabase(testDbPath)

	db, err := ConnectDb(testDbPath)

	defer dropDatabase(testDbPath)
	if err != nil {
		assert.Fail("Problem with creation db connection:" + err.Error())
		return
	}

	assert.True(db.IsConnectionOpened())

	db.Disconnect()

	assert.False(db.IsConnectionOpened())
}

func TestSanitizeString(t *testing.T) {
	assert := require.New(t)
	db := createDbAndConnect(t, assert)
	defer clearDb()
	if db == nil {
		t.Fail()
		return
	}
	defer db.Disconnect()

	testText := "text'test''test\"test\\"

	db.SetDatabaseVersion(testText)
	assert.Equal(testText, db.GetDatabaseVersion())
}

func TestDatabaseVersion(t *testing.T) {
	assert := require.New(t)
	db := createDbAndConnect(t, assert)
	defer clearDb()
	if db == nil {
		t.Fail()
		return
	}

	{
		version := db.GetDatabaseVersion()
		assert.Equal(latestVersion, version)
	}

	{
		db.SetDatabaseVersion("1.0")
		version := db.GetDatabaseVersion()
		assert.Equal("1.0", version)
	}

	db.Disconnect()

	{
		db = connectDb(t)
		version := db.GetDatabaseVersion()
		assert.Equal("1.0", version)
		db.Disconnect()
	}

	{
		db = connectDb(t)
		db.SetDatabaseVersion("1.2")
		db.Disconnect()
	}

	{
		db = connectDb(t)
		version := db.GetDatabaseVersion()
		assert.Equal("1.2", version)
		db.Disconnect()
	}
}

func TestGetUserLimits(t *testing.T) {
	assert := require.New(t)
	db := createDbAndConnect(t, assert)
	defer clearDb()
	if db == nil {
		t.Fail()
		return
	}
	defer db.Disconnect()

	var token1 = "321"
	var token2 = "123"

	db.SetUserLimits(token1, 1, 2, 3)
	assert.True(db.DoesUserExist(token1))

	retentionLimitMinutes, maxSizeBytes, shareCreationLimitMinutes := db.GetUserLimits(token1)

	assert.False(db.DoesUserExist(token2))
	assert.Equal(1, retentionLimitMinutes)
	assert.Equal(2, maxSizeBytes)
	assert.Equal(3, shareCreationLimitMinutes)
}

func TestRemoveUserLimits(t *testing.T) {
	assert := require.New(t)
	db := createDbAndConnect(t, assert)
	defer clearDb()
	if db == nil {
		t.Fail()
		return
	}
	defer db.Disconnect()

	var token1 = "321"
	var token2 = "123"

	db.SetUserLimits(token1, 1, 2, 3)
	assert.True(db.DoesUserExist(token1))

	db.RemoveUserByToken(token1)
	assert.False(db.DoesUserExist(token1))

	db.RemoveUserByToken(token2)
	assert.False(db.DoesUserExist(token2))
}

func TestSaveAndConsumeMessage(t *testing.T) {
	assert := require.New(t)
	db := createDbAndConnect(t, assert)
	defer clearDb()
	if db == nil {
		t.Fail()
		return
	}
	defer db.Disconnect()

	var message1 = "test message 1"
	var message2 = "test message 2"
	var message3 = "test message 3"

	var messageToken1 = "321"
	var messageToken2 = "123"

	err := db.SaveMessage(messageToken1, 100, message1)
	assert.Nil(err)
	err = db.SaveMessage(messageToken1, 100, message2)
	assert.NotNil(err)
	err = db.SaveMessage(messageToken2, 100, message3)
	assert.Nil(err)

	{
		message := db.TryConsumeMessage(messageToken1)
		assert.Equal(message1, *message)
	}

	{
		message := db.TryConsumeMessage(messageToken1)
		assert.Nil(message)
	}

	{
		message := db.TryConsumeMessage(messageToken2)
		assert.Equal(message3, *message)
	}

	{
		message := db.TryConsumeMessage(messageToken2)
		assert.Nil(message)
	}

	{
		message := db.TryConsumeMessage("not existing token")
		assert.Nil(message)
	}
}

func TestUserLastMessageCreationTime(t *testing.T) {
	assert := require.New(t)
	db := createDbAndConnect(t, assert)
	defer clearDb()
	if db == nil {
		t.Fail()
		return
	}
	defer db.Disconnect()

	token := "123"

	{
		lastTime := db.GetUserLastMessageCreationTime(token)
		assert.Equal(int64(0), lastTime)
	}

	{
		db.SetUserLastMessageCreationTime(token, 100)
		lastTime := db.GetUserLastMessageCreationTime(token)
		assert.Equal(int64(100), lastTime)
	}

	{
		db.SetUserLastMessageCreationTime(token, 200)
		lastTime := db.GetUserLastMessageCreationTime(token)
		assert.Equal(int64(200), lastTime)
	}
}
