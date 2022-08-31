package test

import (
	"reflect"
	"strconv"
	"testing"
	"time"
)

// MockRemoteDataObject struct contains contents returned as part of RemoteData Client response
type MockRemoteDataObject struct {
	Etag         *string
	Key          *string
	LastModified *time.Time
	Size         *int64
	StorageClass *string
}

// MockRemoteDataClient is used to store all the objects for an app source
type MockRemoteDataClient struct {
	Objects []*MockRemoteDataObject
}

// MockRemoteDataClientDownloadClient is a mock download client
type MockRemoteDataClientDownloadClient struct {
	RemoteFile      string
	DownloadSuccess bool
}

func checkRemoteDataListResponse(t *testing.T, testMethod string, gotObjects, wantObjects []*MockRemoteDataObject, appSourceName string) {
	if !reflect.DeepEqual(gotObjects, wantObjects) {
		for n, gotObject := range gotObjects {
			if *gotObject.Etag != *wantObjects[n].Etag {
				t.Errorf("%s GotResponse[%s] Etag=%s; want %s", testMethod, appSourceName, *gotObject.Etag, *wantObjects[n].Etag)
			}
			if *gotObject.Key != *wantObjects[n].Key {
				t.Errorf("%s GotResponse[%s] Key=%s; want %s", testMethod, appSourceName, *gotObject.Key, *wantObjects[n].Key)
			}
			if *gotObject.StorageClass != *wantObjects[n].StorageClass {
				t.Errorf("%s GotResponse[%s] StorageClass=%s; want %s", testMethod, appSourceName, *gotObject.StorageClass, *wantObjects[n].StorageClass)
			}
			if *gotObject.Size != *wantObjects[n].Size {
				t.Errorf("%s GotResponse[%s] Size=%d; want %d", testMethod, appSourceName, *gotObject.Size, *wantObjects[n].Size)
			}
			if *gotObject.LastModified != *wantObjects[n].LastModified {
				t.Errorf("%s GotResponse[%s] LastModified=%s; want %s", testMethod, appSourceName, gotObject.LastModified.String(), wantObjects[n].LastModified.String())
			}
		}
	}
}

// MockRemDataClntDownloadHandler is a mock handler to check mock download response
type MockRemDataClntDownloadHandler struct {
	WantLocalToRemoteFileMap map[string]MockRemoteDataClientDownloadClient
	GotLocalToRemoteFileMap  map[string]MockRemoteDataClientDownloadClient
}

// AddObjects adds objects to MockRemoteDataClientDownloadHandler
func (c *MockRemDataClntDownloadHandler) AddObjects(localFiles []string, objects ...MockRemoteDataClientDownloadClient) {
	for n := range objects {
		mockMinioDownloadClient := objects[n]
		localFile := localFiles[n]
		if c.WantLocalToRemoteFileMap == nil {
			c.WantLocalToRemoteFileMap = make(map[string]MockRemoteDataClientDownloadClient)
		}
		c.WantLocalToRemoteFileMap[localFile] = mockMinioDownloadClient
	}
}

// CheckRemDataClntDownloadResponse checks if the received object is same as the one we expect
func (c *MockRemDataClntDownloadHandler) CheckRemDataClntDownloadResponse(t *testing.T, testMethod string) {
	if len(c.WantLocalToRemoteFileMap) != len(c.GotLocalToRemoteFileMap) {
		t.Fatalf("%s got %d Responses; want %d", testMethod, len(c.GotLocalToRemoteFileMap), len(c.WantLocalToRemoteFileMap))
	}

	for localFile, gotObject := range c.GotLocalToRemoteFileMap {
		wantObject := c.WantLocalToRemoteFileMap[localFile]
		if wantObject.RemoteFile != gotObject.RemoteFile || wantObject.DownloadSuccess != gotObject.DownloadSuccess {
			t.Errorf("[%s] Want: {RemoteFile=%s, DownloadSuccess=True}, Got: {RemoteFile=%s, DownloadSuccess=%s}", testMethod, wantObject.RemoteFile, gotObject.RemoteFile, strconv.FormatBool(gotObject.DownloadSuccess))
		}
	}
}
