// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"fmt"
	"io/fs"
	"os"
	"sync"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v2"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
)

// AppInstallWorkerPool is a pool of workers to install apps in parallel
type AppInstallWorkerPool struct {
	NumOfWorkers       uint64
	JobList            []*enterpriseApi.AppDeploymentInfo
	JobsChan           chan *enterpriseApi.AppDeploymentInfo
	DownloadPath       string
	Scope              string
	S3ClientMgr        S3ClientManager
	S3Reponse          splclient.S3Response
	wg                 *sync.WaitGroup
	availableDiskSpace uint64
	mu                 sync.Mutex
}

// NewAppInstallWorkerPool returns a new worker pool
func NewAppInstallWorkerPool(numOfWorkers uint64, jobs []enterpriseApi.AppDeploymentInfo,
	s3ClientMgr S3ClientManager, s3Response splclient.S3Response,
	availableDiskSpace uint64, downloadPath, scope string) *AppInstallWorkerPool {
	w := &AppInstallWorkerPool{
		NumOfWorkers:       numOfWorkers,
		JobList:            getObjectsAsPointers(jobs).([]*enterpriseApi.AppDeploymentInfo),
		JobsChan:           make(chan *enterpriseApi.AppDeploymentInfo),
		DownloadPath:       downloadPath,
		Scope:              scope,
		S3ClientMgr:        s3ClientMgr,
		S3Reponse:          s3Response,
		availableDiskSpace: availableDiskSpace,
		wg:                 new(sync.WaitGroup),
	}
	return w
}

// Run is the main work that workers do to install the apps.
// Workers wait for the jobs on the jobs channel and pick them up as they come in
func (w *AppInstallWorkerPool) Run(id int, jobs <-chan *enterpriseApi.AppDeploymentInfo) {
	scopedLog := log.WithName("AppInstallWorkerPool.run()").WithValues("worker id", id)
	for job := range jobs {
		scopedLog.Info("Started install of app", "appName", job.AppName)

		object := getRemoteObjectFromS3Response(job.AppName, w.S3Reponse)
		localFile := getLocalAppFileName(w.DownloadPath, job.AppName, *object.Etag)
		remoteFile := *object.Key

		// download the app from remote storage
		err := w.S3ClientMgr.DownloadApp(remoteFile, localFile, *object.Etag)
		if err != nil {
			scopedLog.Error(err, "unable to download app", "appName", job.AppName)
			setAppInstallState(job, enterpriseApi.DownloadError)
		} else {
			// download is successfull, update the state
			setAppInstallState(job, enterpriseApi.DownloadComplete)

			//TODO: gaurav, need to fill in below code
			// copy the app to the splunk pod

			// untar it, if it is cluster scoped

			// if everything is successful, delete the app from operator pod
		}

		scopedLog.Info("finished job")
		w.mu.Lock()
		w.availableDiskSpace = w.availableDiskSpace + job.Size
		scopedLog.Info("availableDiskSpace after freeing up space", "availableDiskSpace", w.availableDiskSpace)
		w.mu.Unlock()

		w.wg.Done()
	}
}

// isAppAlreadyDownloaded checks if the app is already downloaded on the operator pod by checking a couple of things -
// 1. is the app bundle present on the operator pod &&
// 2. if the size on disk matches the size reported from remote storage
func (w *AppInstallWorkerPool) isAppAlreadyDownloaded(app *enterpriseApi.AppDeploymentInfo) bool {
	scopedLog := log.WithName("isAppAlreadyDownloaded").WithValues("app name", app.AppName)
	object := getRemoteObjectFromS3Response(app.AppName, w.S3Reponse)
	localAppFileName := getLocalAppFileName(w.DownloadPath, app.AppName, *object.Etag)

	var fileInfo fs.FileInfo
	var err error
	// 1. First check if the app is present on operator pod
	if fileInfo, err = os.Stat(localAppFileName); os.IsNotExist(err) {
		scopedLog.Info("App not present on operator pod")
		return false
	}

	// 2. Now, since the app is present, check the size
	localSize := fileInfo.Size()
	remoteSize := *object.Size
	if localSize != remoteSize {
		err = fmt.Errorf("size of app on operator pod does not match size on remote storage. localSize: %d, remoteSize:%d", localSize, remoteSize)
		scopedLog.Error(err, "need to re-download app")
		return false
	}

	// If we reached here, this means app was downloaded successfuly to operator pod,
	// mark this as DownloadComplete
	setAppInstallState(app, enterpriseApi.DownloadComplete)
	return true
}

// Start kicks off the workers to do the job of app installation
func (w *AppInstallWorkerPool) Start() {
	// start the workers
	for i := 1; i <= int(w.NumOfWorkers); i++ {
		go w.Run(i, w.JobsChan)
	}

	var jobsAdded int
	// add the apps to the jobs channel so that workers can pick them up
	for {
		for index := 0; index < len(w.JobList); index++ {
			app := w.JobList[index]

			switch getAppInstallState(app) {
			case enterpriseApi.DownloadNotStarted:
				// check if we have already downloaded the app
				if w.isAppAlreadyDownloaded(app) {
					//TODO: check for app copy and untar state
					continue
				}
				// only proceed if we have enough disk space to download this app
				if w.availableDiskSpace-app.Size > 0 {
					w.availableDiskSpace = w.availableDiskSpace - app.Size
					w.wg.Add(1)

					// set the app install state as DownloadInProgress
					setAppInstallState(app, enterpriseApi.DownloadInProgress)

					// add the job to the jobs channel
					w.JobsChan <- app
					jobsAdded++
				} else {
					_ = fmt.Errorf("not enough space to accomodate app:%s, availableDiskSpace:%d", app.AppName, w.availableDiskSpace)
					continue
				}
			case enterpriseApi.DownloadComplete:
				// This is done in case if all the apps were already downloaded, we would need to
				// break from outer loop and close the channel as there is no job to add.
				jobsAdded++
				continue
			default:
				continue
			}
		}

		// all the jobs are added to the channel, we are done here
		if jobsAdded == len(w.JobList) {
			break
		}
	}

	// close the jobs channel now
	close(w.JobsChan)

	// wait for all the workers to finish their jobs
	w.wg.Wait()
}
